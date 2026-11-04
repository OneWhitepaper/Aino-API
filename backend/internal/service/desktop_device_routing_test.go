package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/dgraph-io/ristretto"
	"github.com/stretchr/testify/require"
)

func TestDesktopDeviceRequestSelectsIsolatedBillingGroup(t *testing.T) {
	models, _ := desktopImageFixture(t)
	source := &APIKey{ID: 2, UserID: 17, DesktopCredentialType: "device", User: &User{ID: 17}}
	selected, err := models.ResolveRequest(context.Background(), source, "image", "images_generations", []string{"fixture-image"}, DesktopPurposeImage, true)
	require.NoError(t, err)
	require.Equal(t, int64(11), *selected.Key.GroupID)
	require.Equal(t, DesktopPurposeImage, selected.Key.DesktopPurpose)
	require.Nil(t, source.GroupID)
	require.Nil(t, source.Group)
	selected.Key.Group.ModelAllowlist.Models[0] = "modified"
	again, err := models.ResolveRequest(context.Background(), source, "image", "images_generations", []string{"fixture-image"}, "", true)
	require.NoError(t, err)
	require.Equal(t, []string{"fixture-image"}, again.Key.Group.ModelAllowlist.Models)
	for _, args := range []struct {
		id, endpoint, purpose string
		names                 []string
	}{
		{"", "images_generations", "", []string{"fixture-image"}},
		{"image", "chat_completions", "", []string{"fixture-image"}},
		{"image", "images_generations", "chat", []string{"fixture-image"}},
		{"image", "images_generations", "", []string{"fixture-image", "other"}},
		{"image", "images_generations", "", nil},
	} {
		_, err := models.ResolveRequest(context.Background(), source, args.id, args.endpoint, args.names, args.purpose, true)
		require.Error(t, err)
	}
}

func TestImageTaskKeepsPrivateOriginalBillingRoute(t *testing.T) {
	store := &imageTaskMemoryStore{}
	tasks := NewImageTaskService(store)
	groupID := int64(11)
	key := &APIKey{ID: 2, UserID: 17, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformOpenAI, SubscriptionType: SubscriptionTypeSubscription}}
	sub := &UserSubscription{ID: 21, UserID: 17, GroupID: 11, ExpiresAt: time.Now().Add(time.Hour)}
	task, err := tasks.Create(context.Background(), ImageTaskOwner{UserID: 17, APIKeyID: 2}, ImageTaskRouteFromRequest(key, sub))
	require.NoError(t, err)
	key.Group.Platform = PlatformGrok
	route, err := tasks.GetRoute(context.Background(), ImageTaskOwner{UserID: 17, APIKeyID: 2}, task.ID)
	require.NoError(t, err)
	require.Equal(t, PlatformOpenAI, route.Platform)
	require.Equal(t, int64(21), route.Subscription.ID)
	_, err = tasks.GetRoute(context.Background(), ImageTaskOwner{UserID: 17, APIKeyID: 3}, task.ID)
	require.ErrorIs(t, err, ErrImageTaskNotFound)
}

func TestDesktopProjectionHasHardAgeAndCannotFreezeBalance(t *testing.T) {
	models, keys := desktopImageFixture(t)
	cache, err := ristretto.NewCache(&ristretto.Config{NumCounters: 1000, MaxCost: 1000, BufferItems: 64})
	require.NoError(t, err)
	defer cache.Close()
	keys.authCacheL1 = cache
	key := &APIKey{ID: 2, UserID: 17, User: &User{ID: 17}}
	first, err := models.ResolveRequest(context.Background(), key, "image", "images_generations", []string{"fixture-image"}, "", true)
	require.NoError(t, err)
	require.Equal(t, int64(11), *first.Key.GroupID)
	cache.Wait()
	cached, ok := cache.Get("desktop:catalog:17")
	require.True(t, ok)
	projection, ok := cached.(desktopCatalogProjection)
	require.True(t, ok)
	require.NotContains(t, string(projection.Data), `"Balance"`)
	// Model entitlement is reusable briefly; quota/balance is never reused as a
	// permission. Permission revocation must be authoritative at the hard age.
	groups, ok := keys.groupRepo.(*desktopGroupRepo)
	require.True(t, ok)
	groups.groups[0].AllowImageGeneration = false
	_, err = models.ResolveRequest(context.Background(), key, "image", "images_generations", []string{"fixture-image"}, "", true)
	require.NoError(t, err)
	projection.CheckedAt = time.Now().Add(-16 * time.Second)
	cache.SetWithTTL("desktop:catalog:17", projection, 1, time.Minute)
	cache.Wait()
	_, err = models.ResolveRequest(context.Background(), key, "image", "images_generations", []string{"fixture-image"}, "", true)
	require.Error(t, err, "a stale projection cannot extend withdrawn image permissions")
}

func TestDesktopCatalogBalanceChangesWithoutProjectionRefresh(t *testing.T) {
	models, keys := desktopImageFixture(t)
	models.eligibility.cfg.RunMode = config.RunModeStandard
	cache, err := ristretto.NewCache(&ristretto.Config{NumCounters: 1000, MaxCost: 1000, BufferItems: 64})
	require.NoError(t, err)
	defer cache.Close()
	keys.authCacheL1 = cache
	before, err := models.ListForUser(context.Background(), 17)
	require.NoError(t, err)
	require.Equal(t, "available", before[0].State)
	cache.Wait()
	users, ok := keys.userRepo.(*desktopUserRepo)
	require.True(t, ok)
	users.user.Balance = 0
	after, err := models.ListForUser(context.Background(), 17)
	require.NoError(t, err)
	require.Equal(t, "insufficient_balance", after[0].State)
}

func TestDeviceRequestUsesBillingBalanceRatherThanIdentitySnapshot(t *testing.T) {
	models, keys := desktopImageFixture(t)
	models.eligibility.cfg.RunMode = config.RunModeStandard
	key := &APIKey{ID: 2, UserID: 17, User: &User{ID: 17, Balance: 0}}
	selected, err := models.ResolveRequest(context.Background(), key, "image", "images_generations", []string{"fixture-image"}, "", true)
	require.NoError(t, err)
	require.Equal(t, 10.0, selected.Key.User.Balance, "a recharge must not be hidden by an older identity snapshot")
	users, ok := keys.userRepo.(*desktopUserRepo)
	require.True(t, ok)
	users.user.Balance = 0
	selected, err = models.ResolveRequest(context.Background(), key, "image", "images_generations", []string{"fixture-image"}, "", true)
	require.NoError(t, err)
	require.Zero(t, selected.Key.User.Balance)
	require.Zero(t, key.User.Balance)
}

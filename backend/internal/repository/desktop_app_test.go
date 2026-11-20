package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestDesktopApplicationFilteringBeforePaginationAndConcurrencySort(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	u := mustCreateAPIKeyRepoUser(t, ctx, client, "desktop-app-filter@test.com")
	otherUser := mustCreateAPIKeyRepoUser(t, ctx, client, "desktop-app-filter-other@test.com")
	create := func(userID int64, app, name string, managed bool) int64 {
		key := &service.APIKey{UserID: userID, DesktopApp: app, DesktopManaged: managed, Key: "fixture-" + name, Name: name, Status: service.StatusActive}
		require.NoError(t, repo.Create(ctx, key))
		return key.ID
	}
	create(u.ID, "", "aino-one", true)
	momoFirst := create(u.ID, "momo", "momo-one", true)
	create(u.ID, "", "aino-two", true)
	momoSecond := create(u.ID, "momo", "momo-two", true)
	future := create(u.ID, "future_app", "future-one", true)
	create(u.ID, "", "momo-lookalike-manual", false)
	create(otherUser.ID, "momo", "another-user-momo", true)
	archived := create(u.ID, "momo", "archived-momo", true)
	require.NoError(t, repo.Delete(ctx, archived))
	svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, nil)
	managed := true
	for _, sortBy := range []string{"id", "current_concurrency"} {
		for page := 1; page <= 2; page++ {
			keys, result, err := svc.List(ctx, u.ID, pagination.PaginationParams{Page: page, PageSize: 1, SortBy: sortBy, SortOrder: "asc"}, service.APIKeyListFilters{DesktopManaged: &managed, DesktopApp: " MoMo "})
			require.NoError(t, err)
			require.EqualValues(t, 2, result.Total)
			require.Equal(t, 2, result.Pages)
			require.Len(t, keys, 1)
			require.Equal(t, []int64{momoFirst, momoSecond}[page-1], keys[0].ID)
			require.Equal(t, "momo", keys[0].DesktopApp)
		}
	}
	keys, result, err := svc.List(ctx, u.ID, pagination.PaginationParams{Page: 1, PageSize: 10}, service.APIKeyListFilters{DesktopApp: service.DesktopAppOther})
	require.NoError(t, err)
	require.EqualValues(t, 1, result.Total)
	require.Equal(t, future, keys[0].ID)
	keys, result, err = svc.List(ctx, u.ID, pagination.PaginationParams{Page: 1, PageSize: 10}, service.APIKeyListFilters{DesktopApp: "momo", IncludeArchived: true})
	require.NoError(t, err)
	require.EqualValues(t, 3, result.Total)
	require.Len(t, keys, 3)
	metadata, err := repo.GetUsageMetadata(ctx, archived)
	require.NoError(t, err)
	require.Equal(t, "momo", metadata.DesktopApp)
	require.Empty(t, metadata.Key)
}

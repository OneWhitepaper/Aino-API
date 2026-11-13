//go:build unit

package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAsyncFundingSnapshotRestoresAcceptedBillingIdentityWithoutCredentials(t *testing.T) {
	key := &APIKey{ID: 7, UserID: 9, Key: "must-not-persist-credential", Quota: 12, RateLimit5h: 3, DesktopManaged: true, DesktopUsage: &DesktopUsage{CallID: "accepted-call", Purpose: "image"}, User: &User{ID: 9, PasswordHash: "must-not-persist-password"}, Group: &Group{ID: 4, Platform: PlatformGrok, Name: "accepted group"}, Funding: &FundingDecision{Source: TeamFundingSourceBalance, PayerUserID: 11, TeamID: 13, ReservationID: 17, RequestID: "accepted-operation", ReservedAmount: "1.00000000", AdmissionToken: "must-not-persist-admission"}}
	snapshot := AsyncFundingSnapshotFromRequest(key)
	require.NotNil(t, snapshot)
	payload, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "must-not-persist")
	// Later key and membership settings must not reinterpret a dispatched bill.
	key.Quota = 0
	key.RateLimit5h = 0
	key.Funding = nil
	key.Group.Name = "new group"
	key.DesktopUsage.Purpose = "chat"
	restored, _, err := RestoreAsyncFundingSnapshot(key, snapshot)
	require.NoError(t, err)
	require.Equal(t, int64(11), restored.Funding.PayerUserID)
	require.Equal(t, "accepted-operation", restored.Funding.RequestID)
	require.Equal(t, 12.0, restored.Quota)
	require.Equal(t, 3.0, restored.RateLimit5h)
	require.Equal(t, "accepted group", restored.Group.Name)
	require.True(t, restored.DesktopManaged)
	require.Equal(t, "image", restored.DesktopUsage.Purpose)
	foreign := *key
	foreign.ID++
	_, _, err = RestoreAsyncFundingSnapshot(&foreign, snapshot)
	require.ErrorIs(t, err, ErrTeamFundingNotFound)
}

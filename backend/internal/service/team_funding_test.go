//go:build unit

package service

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTeamFundingDecimalValidation(t *testing.T) {
	for _, value := range []string{"-1", "NaN", "1e2", "0.000000001", "1000000000000", "", " 1"} {
		_, err := ParseTeamFundingAmount(value)
		require.Error(t, err, value)
	}
	for _, value := range []string{"0", "0.00000001", "100.125", "999999999999.99999999"} {
		amount, err := ParseTeamFundingAmount(value)
		require.NoError(t, err, value)
		require.Equal(t, 8, len(amount.StringFixed(8))-len(amount.Truncate(0).String())-1)
	}
}

func TestTeamFundingPersonalConsentAndModes(t *testing.T) {
	for _, mode := range []string{"personal_first", "personal_only", "team_first", "team_only", "subscription_first", "subscription_only"} {
		require.NoError(t, ValidateFundingPreferences(FundingPreferences{Mode: mode, AllowPersonalBalance: false, AllowPersonalSubscription: false}))
	}
	require.Error(t, ValidateFundingPreferences(FundingPreferences{Mode: "balance_first"}))
	require.Error(t, ValidateFundingPreferences(FundingPreferences{Mode: "team_only", TeamID: ptrTeamID(-1)}))
}
func ptrTeamID(v int64) *int64 { return &v }

func TestTeamFundingNullablePatchDistinguishesNullFromOmission(t *testing.T) {
	var omitted, explicit TeamMemberPatch
	require.NoError(t, json.Unmarshal([]byte(`{"status":"paused"}`), &omitted))
	require.NoError(t, json.Unmarshal([]byte(`{"spend_limit":null,"shared_mode":null}`), &explicit))
	require.False(t, omitted.SpendLimit.Set)
	require.True(t, explicit.SpendLimit.Set)
	require.Nil(t, explicit.SpendLimit.Value)
	require.True(t, explicit.SharedMode.Set)
	require.NoError(t, ValidateTeamMemberPatch(&explicit))
	var zero TeamMemberPatch
	require.NoError(t, json.Unmarshal([]byte(`{"spend_limit":"0"}`), &zero))
	require.NoError(t, ValidateTeamMemberPatch(&zero))
	require.Equal(t, "0.00000000", *zero.SpendLimit.Value)
}

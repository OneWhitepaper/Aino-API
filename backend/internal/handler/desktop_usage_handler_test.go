package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserUsageAdvertisesAcceptedPurposeFiltersAndPreservesOwner(t *testing.T) {
	repo := &userUsageRepoCapture{}
	router := newUserUsageRequestTypeTestRouter(repo)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Data struct {
			Purposes []string `json:"supported_desktop_purposes"`
			Pages    int      `json:"pages"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Contains(t, body.Data.Purposes, "session_summary")
	require.GreaterOrEqual(t, body.Data.Pages, 1)
	for _, purpose := range body.Data.Purposes {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage?user_id=99&desktop_purpose="+purpose, nil))
		require.Equal(t, http.StatusOK, rec.Code, purpose)
		require.Equal(t, int64(42), repo.listFilters.UserID)
		require.Equal(t, purpose, repo.listFilters.DesktopPurpose)
	}
}

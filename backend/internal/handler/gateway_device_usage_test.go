package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestDeviceUsageAggregatesWithoutInventingCurrentPlan(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	writeDesktopDeviceUsage(c, &service.APIKey{ID: 2}, gin.H{"total": gin.H{"actual_cost": 3}}, nil, nil)
	recorder := c.Writer
	require.Equal(t, 200, recorder.Status())
	require.Contains(t, w.Body.String(), `"billing_scope":"device"`)
	require.NotContains(t, w.Body.String(), "planName")
	require.NotContains(t, w.Body.String(), "balance")
}

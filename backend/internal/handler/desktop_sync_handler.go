package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *DesktopHandler) SetSyncService(sync *service.DesktopSyncService) { h.sync = sync }
func (h *DesktopHandler) syncOwner(c *gin.Context) (int64, bool) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return 0, false
	}
	if h.sync == nil {
		response.ErrorFrom(c, service.ErrServiceUnavailable)
		return 0, false
	}
	return subject.UserID, true
}
func (h *DesktopHandler) SyncExchange(c *gin.Context) {
	userID, ok := h.syncOwner(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.DesktopSyncRequestBytes)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || !utf8.Valid(raw) {
		response.ErrorFrom(c, service.ErrDesktopSyncInvalid)
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var input service.DesktopSyncRequest
	if err := decoder.Decode(&input); err != nil {
		response.ErrorFrom(c, service.ErrDesktopSyncInvalid)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		response.ErrorFrom(c, service.ErrDesktopSyncInvalid)
		return
	}
	out, err := h.sync.Exchange(c.Request.Context(), userID, input)
	if response.ErrorFrom(c, err) {
		return
	}
	c.PureJSON(http.StatusOK, response.Response{Code: 0, Message: "success", Data: out})
}
func (h *DesktopHandler) PutSyncBlob(c *gin.Context) {
	userID, ok := h.syncOwner(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.DesktopSyncDocumentBytes)
	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		response.ErrorFrom(c, service.ErrDesktopSyncInvalid)
		return
	}
	if response.ErrorFrom(c, h.sync.PutBlob(c.Request.Context(), userID, c.Param("sha256"), data)) {
		return
	}
	response.Success(c, gin.H{"sha256": c.Param("sha256")})
}
func (h *DesktopHandler) GetSyncBlob(c *gin.Context) {
	userID, ok := h.syncOwner(c)
	if !ok {
		return
	}
	data, err := h.sync.GetBlob(c.Request.Context(), userID, c.Param("sha256"))
	if response.ErrorFrom(c, err) {
		return
	}
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "application/octet-stream", data)
}

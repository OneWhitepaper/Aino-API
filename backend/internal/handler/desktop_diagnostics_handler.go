package handler

import (
	"encoding/base64"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *DesktopHandler) SetDiagnosticsService(diagnostics *service.DiagnosticsService) {
	h.diagnostics = diagnostics
}

func (h *DesktopHandler) diagnosticService(c *gin.Context) *service.DiagnosticsService {
	c.Header("Cache-Control", "no-store")
	if h.diagnostics == nil {
		response.ErrorFrom(c, service.ErrServiceUnavailable)
	}
	return h.diagnostics
}

// UploadDiagnostics always derives ownership from the authenticated account, never the body.
func (h *DesktopHandler) UploadDiagnostics(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	svc := h.diagnosticService(c)
	if svc == nil {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(base64.StdEncoding.EncodedLen(service.MaxDiagnosticCompressedBytes)+1024))
	var input struct {
		Bundle string `json:"bundle_base64" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.ErrorFrom(c, service.ErrInvalidDiagnosticBundle)
		return
	}
	metadata, err := svc.Upload(c.Request.Context(), subject.UserID, input.Bundle)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Created(c, metadata)
}

func (h *DesktopHandler) GetOwnDiagnostic(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	svc := h.diagnosticService(c)
	if svc == nil {
		return
	}
	record, err := svc.Get(c.Request.Context(), c.Param("id"), subject.UserID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, record.DiagnosticMetadata)
}

func (h *DesktopHandler) DeleteOwnDiagnostic(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	svc := h.diagnosticService(c)
	if svc == nil {
		return
	}
	if response.ErrorFrom(c, svc.Delete(c.Request.Context(), c.Param("id"), subject.UserID)) {
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

// The admin route group supplies the existing administrator auth and sensitive-read audit.
func (h *DesktopHandler) ListDiagnostics(c *gin.Context) {
	svc := h.diagnosticService(c)
	if svc == nil {
		return
	}
	page, e1 := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, e2 := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if e1 != nil || e2 != nil || page < 1 || page > 100000 || size < 1 || size > 100 {
		response.BadRequest(c, "Invalid pagination")
		return
	}
	items, total, err := svc.List(c.Request.Context(), page, size, c.Query("id"))
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

func (h *DesktopHandler) ReadDiagnostic(c *gin.Context) {
	svc := h.diagnosticService(c)
	if svc == nil {
		return
	}
	record, err := svc.Get(c.Request.Context(), c.Param("id"), 0)
	if response.ErrorFrom(c, err) {
		return
	}
	envelope, err := service.DecodeDiagnosticBundle(record.Bundle)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, gin.H{"metadata": record.DiagnosticMetadata, "files": envelope.Files})
}

func (h *DesktopHandler) DeleteDiagnostic(c *gin.Context) {
	svc := h.diagnosticService(c)
	if svc == nil {
		return
	}
	if response.ErrorFrom(c, svc.Delete(c.Request.Context(), c.Param("id"), 0)) {
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

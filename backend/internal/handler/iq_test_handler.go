package handler

import (
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/iqtest"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type IQTestHandler struct {
	service *service.IQTestService
	cfg     *config.Config
}

func NewIQTestHandler(service *service.IQTestService, cfg *config.Config) *IQTestHandler {
	return &IQTestHandler{service: service, cfg: cfg}
}

func iqTestUser(c *gin.Context) (int64, bool) {
	c.Header("Cache-Control", "private, no-store")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return 0, false
	}
	return subject.UserID, true
}

func (h *IQTestHandler) clientIP(c *gin.Context) string {
	return ip.GetSecurityClientIP(c, h.cfg.TrustForwardedIPForAPIKeyACL())
}

func (h *IQTestHandler) Start(c *gin.Context) {
	userID, ok := iqTestUser(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	var request service.IQTestRequest
	if c.ShouldBindJSON(&request) != nil {
		response.BadRequest(c, "Invalid test settings")
		return
	}
	task, err := h.service.Start(c.Request.Context(), userID, request, h.clientIP(c))
	if response.ErrorFrom(c, err) {
		return
	}
	response.Accepted(c, task)
}

func (h *IQTestHandler) List(c *gin.Context) {
	userID, ok := iqTestUser(c)
	if !ok {
		return
	}
	offset := 0
	if raw := c.Query("offset"); raw != "" {
		var err error
		offset, err = strconv.Atoi(raw)
		if err != nil {
			response.BadRequest(c, "Invalid offset")
			return
		}
	}
	items, more, err := h.service.List(c.Request.Context(), userID, offset)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, gin.H{"items": items, "has_more": more})
}

func validIQTestID(c *gin.Context) bool {
	if _, err := uuid.Parse(c.Param("id")); err != nil {
		response.NotFound(c, "Test not found")
		return false
	}
	return true
}
func (h *IQTestHandler) Get(c *gin.Context) {
	userID, ok := iqTestUser(c)
	if !ok || !validIQTestID(c) {
		return
	}
	task, err := h.service.Get(c.Request.Context(), userID, c.Param("id"))
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, task)
}
func (h *IQTestHandler) Cancel(c *gin.Context) {
	userID, ok := iqTestUser(c)
	if !ok || !validIQTestID(c) {
		return
	}
	task, err := h.service.Cancel(c.Request.Context(), userID, c.Param("id"))
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, task)
}
func (h *IQTestHandler) Models(c *gin.Context) {
	userID, ok := iqTestUser(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	var request service.IQTestConnectionRequest
	if c.ShouldBindJSON(&request) != nil {
		response.BadRequest(c, "Invalid connection settings")
		return
	}
	models, err := h.service.Models(c.Request.Context(), userID, request, h.clientIP(c))
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, models)
}
func (h *IQTestHandler) Keys(c *gin.Context) {
	userID, ok := iqTestUser(c)
	if !ok {
		return
	}
	keys, err := h.service.Keys(c.Request.Context(), userID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, keys)
}

func (h *IQTestHandler) Options(c *gin.Context) {
	if _, ok := iqTestUser(c); !ok {
		return
	}
	response.Success(c, gin.H{"prompts": gin.H{"candy": iqtest.CandyPrompt, "pelican": iqtest.PelicanPrompt}})
}

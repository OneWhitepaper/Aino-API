package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type IQRadarHandler struct{ service *service.IQRadarService }

func NewIQRadarHandler(service *service.IQRadarService) *IQRadarHandler {
	return &IQRadarHandler{service: service}
}

func (h *IQRadarHandler) Get(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, h.service.Snapshot())
}

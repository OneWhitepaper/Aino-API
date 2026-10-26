package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) GetContactPage(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	settings, err := h.settingService.GetContactPage(c.Request.Context(), true)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

func (h *SettingHandler) UpdateContactPage(c *gin.Context) {
	// Twelve 300 KB images plus base64 overhead fit within this bound.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 6*1024*1024)
	body, err := io.ReadAll(c.Request.Body)
	var req *service.ContactPageSettings
	if err == nil {
		err = json.Unmarshal(body, &req)
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			response.Error(c, http.StatusRequestEntityTooLarge, "Contact page images are too large")
			return
		}
		response.BadRequest(c, "Invalid contact page configuration")
		return
	}
	if req == nil {
		response.BadRequest(c, "Contact page configuration is required")
		return
	}
	if err := h.settingService.SetContactPage(c.Request.Context(), *req); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.GetContactPage(c)
}

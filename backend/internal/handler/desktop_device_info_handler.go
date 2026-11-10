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

func decodeDesktopDeviceInfo(raw []byte) (service.DesktopDeviceInfo, error) {
	var info service.DesktopDeviceInfo
	if !utf8.Valid(raw) {
		return info, service.ErrDesktopDeviceInfoInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return info, service.ErrDesktopDeviceInfoInvalid
	}
	fields := map[string]*string{"installation_name": &info.InstallationName, "os": &info.OS, "client_type": &info.ClientType, "client_version": &info.ClientVersion}
	seen := map[string]bool{}
	for decoder.More() {
		name, err := decoder.Token()
		if err != nil {
			return info, service.ErrDesktopDeviceInfoInvalid
		}
		key, valid := name.(string)
		target, known := fields[key]
		if !valid || !known || seen[key] {
			return info, service.ErrDesktopDeviceInfoInvalid
		}
		value, err := decoder.Token()
		text, valid := value.(string)
		if err != nil || !valid {
			return info, service.ErrDesktopDeviceInfoInvalid
		}
		*target, seen[key] = text, true
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') || len(seen) != len(fields) {
		return info, service.ErrDesktopDeviceInfoInvalid
	}
	if _, err := decoder.Token(); err != io.EOF {
		return info, service.ErrDesktopDeviceInfoInvalid
	}
	return info, nil
}

func (h *DesktopHandler) UpdateDeviceInfo(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if h.credentials == nil {
		response.ErrorFrom(c, service.ErrServiceUnavailable)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err != nil {
		response.ErrorFrom(c, service.ErrDesktopDeviceInfoInvalid)
		return
	}
	info, err := decodeDesktopDeviceInfo(raw)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	saved, err := h.credentials.UpdateDeviceInfo(c.Request.Context(), subject.UserID, c.Param("device_id"), info)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, saved)
}

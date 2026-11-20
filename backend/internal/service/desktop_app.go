package service

import (
	"regexp"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	DesktopAppAino  = "aino"
	DesktopAppMoMo  = "momo"
	DesktopAppOther = "other" // list-filter bucket for all other application IDs
)

var desktopAppPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

var ErrDesktopAppConflict = infraerrors.Conflict("DESKTOP_APP_CONFLICT", "existing credential belongs to a different desktop application; use a separate installation ID or connection grant")

// NormalizeDesktopApp leaves omission empty so reusing an existing credential
// preserves its application. Only newly created managed keys default to Aino.
// This is classification metadata, never an authorization or billing policy.
func NormalizeDesktopApp(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value != "" && (len(value) > 64 || !desktopAppPattern.MatchString(value)) {
		return "", infraerrors.BadRequest("INVALID_DESKTOP_APP", "desktop_app must be a snake_case application ID of at most 64 characters")
	}
	return value, nil
}

func DesktopAppForKey(key *APIKey) string {
	if key == nil || !key.DesktopManaged {
		return ""
	}
	if key.DesktopApp == "" {
		return DesktopAppAino
	}
	return key.DesktopApp
}

func DesktopAppKeyName(app string) string {
	switch app {
	case "", DesktopAppAino:
		return "Aino desktop"
	case DesktopAppMoMo:
		return "MoMo desktop"
	default:
		return app + " desktop"
	}
}

func ValidateDesktopAppReuse(requested string, key *APIKey) error {
	if requested != "" && requested != DesktopAppForKey(key) {
		return ErrDesktopAppConflict
	}
	return nil
}

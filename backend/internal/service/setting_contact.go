package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/mail"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	_ "golang.org/x/image/webp"
)

const SettingKeyContactPage = "contact_page"

// Contact page images are stored with their configuration, like the site logo.
// They are only loaded on the contact page, never in public settings or HTML.
type ContactPageSettings struct {
	Description  string          `json:"description"`
	ServiceHours string          `json:"service_hours"`
	Methods      []ContactMethod `json:"methods"`
	Groups       []ContactGroup  `json:"groups"`
}

type ContactMethod struct {
	Type    string `json:"type"`
	Label   string `json:"label"`
	Value   string `json:"value"`
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
}

type ContactGroup struct {
	Platform    string `json:"platform"`
	Name        string `json:"name"`
	Description string `json:"description"`
	QRCode      string `json:"qr_code"`
	GroupNumber string `json:"group_number"`
	JoinURL     string `json:"join_url"`
	Enabled     bool   `json:"enabled"`
}

func (s *SettingService) GetContactPage(ctx context.Context, includeHidden bool) (*ContactPageSettings, error) {
	settings := &ContactPageSettings{Methods: []ContactMethod{}, Groups: []ContactGroup{}}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyContactPage)
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		return nil, fmt.Errorf("get contact page: %w", err)
	}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), settings); err != nil {
			return nil, fmt.Errorf("decode contact page: %w", err)
		}
	}
	if settings.Methods == nil {
		settings.Methods = []ContactMethod{}
	}
	if settings.Groups == nil {
		settings.Groups = []ContactGroup{}
	}
	if !includeHidden {
		settings.Methods = slices.DeleteFunc(settings.Methods, func(m ContactMethod) bool { return !m.Enabled })
		settings.Groups = slices.DeleteFunc(settings.Groups, func(g ContactGroup) bool { return !g.Enabled })
	}
	return settings, nil
}

func (s *SettingService) SetContactPage(ctx context.Context, input ContactPageSettings) error {
	// Work on a copy so failed validation never mutates the caller's draft.
	input.Methods = slices.Clone(input.Methods)
	input.Groups = slices.Clone(input.Groups)
	if err := validateContactPage(&input); err != nil {
		return infraerrors.BadRequest("INVALID_CONTACT_PAGE", err.Error())
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("encode contact page: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeyContactPage, string(encoded)); err != nil {
		return fmt.Errorf("save contact page: %w", err)
	}
	return nil
}

func validateContactPage(settings *ContactPageSettings) error {
	settings.Description = strings.TrimSpace(settings.Description)
	settings.ServiceHours = strings.TrimSpace(settings.ServiceHours)
	if utf8.RuneCountInString(settings.Description) > 2000 || utf8.RuneCountInString(settings.ServiceHours) > 200 {
		return errors.New("contact introduction or service hours is too long")
	}
	if len(settings.Methods) > 12 || len(settings.Groups) > 12 {
		return errors.New("a maximum of 12 contact methods and 12 groups is allowed")
	}
	for i := range settings.Methods {
		m := &settings.Methods[i]
		m.Label, m.Value, m.URL = strings.TrimSpace(m.Label), strings.TrimSpace(m.Value), strings.TrimSpace(m.URL)
		if !slices.Contains([]string{"wechat", "qq", "email", "phone", "link", "other"}, m.Type) {
			return fmt.Errorf("contact %d: unsupported type", i+1)
		}
		if utf8.RuneCountInString(m.Label) > 80 || utf8.RuneCountInString(m.Value) > 300 || (m.Enabled && (m.Label == "" || m.Value == "")) {
			return fmt.Errorf("contact %d: provide a label (up to 80 characters) and value (up to 300 characters)", i+1)
		}
		if !validContactLink(m.URL) || (m.Enabled && m.Type == "link" && m.URL == "") {
			return fmt.Errorf("contact %d: enter an absolute HTTP or HTTPS link without embedded credentials", i+1)
		}
		if m.Type == "email" && m.Value != "" {
			address, err := mail.ParseAddress(m.Value)
			if err != nil || address.Address != m.Value {
				return fmt.Errorf("contact %d: enter a valid email address", i+1)
			}
		}
	}
	for i := range settings.Groups {
		g := &settings.Groups[i]
		g.Name, g.Description = strings.TrimSpace(g.Name), strings.TrimSpace(g.Description)
		g.GroupNumber, g.JoinURL, g.QRCode = strings.TrimSpace(g.GroupNumber), strings.TrimSpace(g.JoinURL), strings.TrimSpace(g.QRCode)
		if !slices.Contains([]string{"wechat", "qq", "other"}, g.Platform) {
			return fmt.Errorf("group %d: unsupported platform", i+1)
		}
		if utf8.RuneCountInString(g.Name) > 80 || utf8.RuneCountInString(g.Description) > 500 || utf8.RuneCountInString(g.GroupNumber) > 100 || (g.Enabled && g.Name == "") {
			return fmt.Errorf("group %d: check the name (80 characters), description (500 characters), and group number (100 characters)", i+1)
		}
		if !validContactLink(g.JoinURL) {
			return fmt.Errorf("group %d: enter an absolute HTTP or HTTPS join link without embedded credentials", i+1)
		}
		if g.Enabled && g.QRCode == "" && g.GroupNumber == "" && g.JoinURL == "" {
			return fmt.Errorf("group %d: provide a QR code, group number, or join link", i+1)
		}
		if g.QRCode != "" && !validContactQRImage(g.QRCode) {
			return fmt.Errorf("group %d: upload a PNG, JPEG, or WebP image up to 300 KB and 4096 × 4096 pixels", i+1)
		}
	}
	return nil
}

func validContactLink(raw string) bool {
	if raw == "" {
		return true
	}
	if len(raw) > 2048 {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil
}

func validContactQRImage(raw string) bool {
	const maxImageBytes = 300 * 1024
	if len(raw) > base64.StdEncoding.EncodedLen(maxImageBytes)+32 {
		return false
	}
	header, data, ok := strings.Cut(raw, ",")
	formats := map[string]string{"data:image/png;base64": "png", "data:image/jpeg;base64": "jpeg", "data:image/webp;base64": "webp"}
	format, supported := formats[header]
	if !ok || !supported {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil || len(decoded) > maxImageBytes {
		return false
	}
	config, actualFormat, err := image.DecodeConfig(bytes.NewReader(decoded))
	return err == nil && actualFormat == format && config.Width > 0 && config.Height > 0 && config.Width <= 4096 && config.Height <= 4096
}

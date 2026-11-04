//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type contactSettingRepo struct{ settingRepoStub }

func (r *contactSettingRepo) Set(_ context.Context, key, value string) error {
	if r.err != nil {
		return r.err
	}
	r.values[key] = value
	return nil
}

const contactTestPNG = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jF9sAAAAASUVORK5CYII="

func TestContactPagePersistsAndOnlyPublishesEnabledEntries(t *testing.T) {
	ctx := context.Background()
	repo := &contactSettingRepo{settingRepoStub{values: map[string]string{"unrelated_setting": "keep"}}}
	s := NewSettingService(repo, &config.Config{})
	initial, err := s.GetContactPage(ctx, true)
	require.NoError(t, err)
	require.Empty(t, initial.Methods)
	require.Empty(t, initial.Groups)
	input := ContactPageSettings{
		Description: " 联系我们 ", ServiceHours: "工作日 09:00–18:00",
		Methods: []ContactMethod{
			{Type: "email", Label: "客服邮箱", Value: "help@example.com", Enabled: true},
			{Type: "wechat", Label: "备用微信", Value: "hidden-contact", Enabled: false},
		},
		Groups: []ContactGroup{
			{Platform: "wechat", Name: "微信交流群", QRCode: contactTestPNG, Enabled: true},
			{Platform: "qq", Name: "QQ 交流群", GroupNumber: "123456", JoinURL: "https://qm.qq.com/example", Enabled: true},
			{Platform: "wechat", Name: "未发布群", QRCode: contactTestPNG, Enabled: false},
		},
	}
	require.NoError(t, s.SetContactPage(ctx, input))
	admin, err := NewSettingService(repo, &config.Config{}).GetContactPage(ctx, true)
	require.NoError(t, err)
	require.Equal(t, "联系我们", admin.Description)
	require.Len(t, admin.Methods, 2)
	require.Len(t, admin.Groups, 3)
	user, err := s.GetContactPage(ctx, false)
	require.NoError(t, err)
	require.Len(t, user.Methods, 1)
	require.Len(t, user.Groups, 2)
	require.Equal(t, contactTestPNG, user.Groups[0].QRCode)
	require.Equal(t, "123456", user.Groups[1].GroupNumber)
	require.Equal(t, "keep", repo.values["unrelated_setting"])
}

func TestContactPageRejectsUnsafeOrOversizedConfigurationAtomically(t *testing.T) {
	for name, input := range map[string]ContactPageSettings{
		"unsafe link":      {Methods: []ContactMethod{{Type: "link", Label: "客服", Value: "网址", URL: "javascript:alert(1)", Enabled: true}}},
		"link credentials": {Groups: []ContactGroup{{Platform: "qq", Name: "QQ群", JoinURL: "https://user:secret@example.com", Enabled: true}}},
		"invalid image":    {Groups: []ContactGroup{{Platform: "wechat", Name: "微信群", QRCode: "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("not an image")), Enabled: true}}},
		"svg image":        {Groups: []ContactGroup{{Platform: "wechat", Name: "微信群", QRCode: "data:image/svg+xml;base64,PHN2Zy8+", Enabled: true}}},
		"oversized image":  {Groups: []ContactGroup{{Platform: "wechat", Name: "微信群", QRCode: "data:image/png;base64," + base64.StdEncoding.EncodeToString(make([]byte, 301*1024)), Enabled: true}}},
		"unknown platform": {Groups: []ContactGroup{{Platform: "invalid", Name: "群聊"}}},
		"missing details":  {Methods: []ContactMethod{{Type: "wechat", Label: "客服", Enabled: true}}},
		"too many groups":  {Groups: make([]ContactGroup, 13)},
		"long description": {Description: strings.Repeat("字", 2001)},
	} {
		t.Run(name, func(t *testing.T) {
			repo := &contactSettingRepo{settingRepoStub{values: map[string]string{SettingKeyContactPage: `{ "description": "keep" }`}}}
			s := NewSettingService(repo, &config.Config{})
			require.Error(t, s.SetContactPage(context.Background(), input))
			require.Equal(t, `{ "description": "keep" }`, repo.values[SettingKeyContactPage])
		})
	}
}

func TestContactPageSurfacesStorageErrors(t *testing.T) {
	repo := &contactSettingRepo{settingRepoStub{values: map[string]string{}, err: errors.New("storage unavailable")}}
	s := NewSettingService(repo, &config.Config{})
	_, err := s.GetContactPage(context.Background(), true)
	require.ErrorContains(t, err, "storage unavailable")
	require.ErrorContains(t, s.SetContactPage(context.Background(), ContactPageSettings{}), "storage unavailable")
}

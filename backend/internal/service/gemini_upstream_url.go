package service

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// geminiAIStudioActions 是允许出现在上游 URL 里的 action 集合，与 ForwardNative
// 的入站校验保持一致，同时避免 compat 侧把可变字符串直接拼进 path。
var geminiAIStudioActions = map[string]struct{}{
	"generateContent":       {},
	"streamGenerateContent": {},
	"countTokens":           {},
}

// buildGeminiAIStudioModelActionURL 组装 AI Studio 的
// /v1beta/models/{model}:{action} 上游 URL。
//
// model 是客户端可控的（native 路由取自 URL 片段，compat 路由取自请求体的 model
// 字段，之后可能再经渠道映射），因此必须先校验并编码为单个路径片段。
// 新增 AI Studio 端点请一律走本函数。
func buildGeminiAIStudioModelActionURL(baseURL, model, action string, stream bool) (string, error) {
	trimmedBase := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmedBase == "" {
		return "", errors.New("gemini base url is required")
	}
	trimmedModel := strings.TrimSpace(model)
	if trimmedModel == "" {
		return "", errors.New("gemini model is required")
	}
	if err := validateGeminiModelPathSegment(trimmedModel); err != nil {
		return "", err
	}
	trimmedAction := strings.TrimSpace(action)
	if _, ok := geminiAIStudioActions[trimmedAction]; !ok {
		return "", fmt.Errorf("unsupported gemini action: %s", trimmedAction)
	}

	fullURL := fmt.Sprintf("%s/v1beta/models/%s:%s", trimmedBase, url.PathEscape(trimmedModel), trimmedAction)
	if stream {
		fullURL += "?alt=sse"
	}
	return fullURL, nil
}

// IsSafeGeminiModelPathSegment 供 handler 层在解析出 URL 里的模型名后立刻校验，
// 让客户端拿到明确的 400，而不是等到构造上游请求时才报错。
func IsSafeGeminiModelPathSegment(model string) bool {
	model = strings.TrimSpace(model)
	if model == "" || len(model) > maxUpstreamPathSegmentLen || !utf8.ValidString(model) {
		return false
	}
	dotsOnly := true
	for _, r := range model {
		// 中转上游的模型别名可能带中文及 [次] 后缀；仍使用闭集允许清单，
		// 不接受路径分隔符、百分号或可改变 URL 结构的字符。
		if (r >= utf8.RuneSelf || !isSafeUpstreamPathSegmentByte(byte(r))) &&
			r != '[' && r != ']' && !unicode.Is(unicode.Han, r) {
			return false
		}
		if r != '.' {
			dotsOnly = false
		}
	}
	return !dotsOnly
}

func validateGeminiModelPathSegment(model string) error {
	if !IsSafeGeminiModelPathSegment(model) {
		return errors.New("invalid gemini model for upstream url path")
	}
	return nil
}

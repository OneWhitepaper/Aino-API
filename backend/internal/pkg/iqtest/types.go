// Package iqtest runs bounded, single-task model checks using the Responses API.
package iqtest

import (
	"regexp"
	"strings"
)

const (
	MaxOutputBytes   = 256 << 10
	MaxResponseBytes = 2 << 20
	CandyPrompt      = "袋子里有苹果糖、桃子糖、西瓜糖，每种糖有圆形和星形两种形状，形状可以用手摸出来。数量如下：苹果圆形7颗、苹果星形7颗；桃子圆形9颗、桃子星形6颗；西瓜圆形8颗、西瓜星形4颗。先确定要取出的糖果数量，再在看不见的情况下任意取出这么多颗（可以摸出形状）。最少取出多少颗，才能保证取出的糖果中有形状不同的一颗苹果糖和一颗桃子糖？请给出推理，并以“最终答案：<整数>”单独一行结束。"
	PelicanPrompt    = "请创作一只骑自行车的鹈鹕，使用可以直接在浏览器运行的独立 HTML 文档（可内嵌 CSS、SVG 和 JavaScript）。画面需要清晰呈现鹈鹕的喙、身体与脚，以及自行车的车架、两个车轮、车把和踏板，构图自然、细节完整。无需外部资源、链接、网络请求或依赖。只输出完整 HTML 代码。"
)

type Connection struct {
	Source   string `json:"source"`
	BaseURL  string `json:"base_url,omitempty"`
	APIKey   string `json:"-"`
	ClientIP string `json:"-"`
}

type RunInput struct {
	Connection
	Kind   string
	Model  string
	Effort string
	Tier   string
	Stream bool
}

type Usage struct {
	InputTokens     int64  `json:"input_tokens"`
	OutputTokens    int64  `json:"output_tokens"`
	CachedTokens    *int64 `json:"cached_tokens"`
	ReasoningTokens *int64 `json:"reasoning_tokens"`
}

type Result struct {
	Output      string `json:"output"`
	Usage       *Usage `json:"usage"`
	DurationMS  int64  `json:"duration_ms"`
	ServiceTier string `json:"service_tier,omitempty"`
}

type Model struct {
	ID      string   `json:"id"`
	Efforts []string `json:"efforts"`
}

var finalAnswer = regexp.MustCompile(`(?i)^(?:(?:最终答案|final answer)\s*[:：]\s*)?([0-9]+)\s*[。.]?$`)

// Grade deliberately checks a final answer, not incidental numbers in reasoning.
// A drawing is never labeled a visual success by a markup/keyword heuristic.
func Grade(kind, output string) (verdict, basis string) {
	if kind == "pelican" {
		return "review", "pelican_manual_review"
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	match := finalAnswer.FindStringSubmatch(strings.TrimSpace(lines[len(lines)-1]))
	if len(match) != 2 {
		return "ungraded", "candy_final_answer_missing"
	}
	if match[1] == "21" {
		return "passed", "candy_final_answer_21"
	}
	return "incorrect", "candy_final_answer_other"
}

// KnownEfforts only supplies capabilities known by this application. Unknown
// models use the upstream default unless the user explicitly supplies an effort.
func KnownEfforts(model string) []string {
	name := strings.ToLower(model)
	switch {
	case strings.HasPrefix(name, "gpt-6.1-sol"):
		return []string{"low", "medium", "high", "xhigh", "max", "ultra"}
	case strings.HasPrefix(name, "gpt-6-astra"):
		return []string{"low", "medium", "high"}
	case strings.HasPrefix(name, "gpt-6-sol"), strings.HasPrefix(name, "gpt-5.6-sol"):
		return []string{"low", "medium", "high", "xhigh"}
	default:
		return []string{}
	}
}

// Package iqradar reads the public Codex Radar benchmark and community feeds.
package iqradar

import "time"

const (
	SourceName = "Codex Radar"
	SourceURL  = "https://codexradar.com/"
	APIBaseURL = "https://api.codexradar.com"
)

type Selection struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

type Catalog struct {
	Benchmark     string      `json:"benchmark"`
	ScoreVersion  string      `json:"score_version"`
	TaskSetSHA256 string      `json:"task_set_sha256"`
	ScoreMax      float64     `json:"score_max"`
	RequiredTasks int         `json:"required_tasks"`
	Selections    []Selection `json:"-"`
}

type HistoryPoint struct {
	Date  string  `json:"date"`
	Score float64 `json:"score"`
}

type Point struct {
	Model            string         `json:"model"`
	Effort           string         `json:"effort"`
	Score            *float64       `json:"score"`
	Coverage         int            `json:"coverage"`
	RequiredTasks    int            `json:"required_tasks"`
	ScoreStatus      string         `json:"score_status"`
	AveragePriceUSD  *float64       `json:"average_price_usd"`
	AverageMinutes   *float64       `json:"average_minutes"`
	PriceSamples     int            `json:"price_samples"`
	DurationSamples  int            `json:"duration_samples"`
	ValidGradeEvents int            `json:"valid_grade_events"`
	LatestGradedAt   *time.Time     `json:"latest_graded_at"`
	FetchedAt        *time.Time     `json:"fetched_at"`
	Stale            bool           `json:"stale"`
	History          []HistoryPoint `json:"history"`
}

type CommunityModel struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Group   string   `json:"group"`
	Average *float64 `json:"average"`
	Count   int      `json:"count"`
}

type Community struct {
	WindowHours int              `json:"window_hours"`
	UpdatedAt   *time.Time       `json:"updated_at"`
	FetchedAt   *time.Time       `json:"fetched_at"`
	Stale       bool             `json:"stale"`
	Models      []CommunityModel `json:"models"`
}

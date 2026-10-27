package iqradar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const maxResponseBytes = 8 << 20

type Client struct {
	http    *http.Client
	apiBase string
	webBase string
}

func NewClient(client *http.Client) *Client {
	if client == nil {
		client = &http.Client{Timeout: 25 * time.Second}
	}
	// Do not follow an upstream redirect to a different host or forward credentials.
	cloned := *client
	cloned.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{http: &cloned, apiBase: APIBaseURL, webBase: strings.TrimRight(SourceURL, "/")}
}

func (c *Client) get(ctx context.Context, endpoint string, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("radar request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("radar fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("radar source returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("radar read: %w", err)
	}
	if len(body) > maxResponseBytes {
		return fmt.Errorf("radar response exceeds size limit")
	}
	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("radar decode: %w", err)
	}
	return nil
}

func (c *Client) Catalog(ctx context.Context) (Catalog, error) {
	var raw struct {
		Retired []string `json:"retired_model_ids"`
		Pool    struct {
			Benchmark string  `json:"benchmark"`
			Version   string  `json:"score_version"`
			Hash      string  `json:"members_sha256"`
			Max       float64 `json:"score_max"`
			Tasks     int     `json:"task_count"`
		} `json:"unified_pool"`
		Collections []struct {
			Selections []Selection `json:"model_effort_selections"`
		} `json:"collections"`
	}
	if err := c.get(ctx, c.apiBase+"/api/v1/library-catalog", &raw); err != nil {
		return Catalog{}, err
	}
	pool := raw.Pool
	if pool.Benchmark == "" || pool.Version == "" || pool.Hash == "" || !finite(pool.Max) || pool.Max <= 0 || pool.Tasks <= 0 {
		return Catalog{}, fmt.Errorf("radar catalog has no valid benchmark definition")
	}
	retired := make(map[string]bool, len(raw.Retired))
	for _, model := range raw.Retired {
		retired[model] = true
	}
	selections := make([]Selection, 0)
	seen := make(map[Selection]bool)
	for _, collection := range raw.Collections {
		for _, selection := range collection.Selections {
			if retired[selection.Model] || seen[selection] || !validIdentifier(selection.Model) || !validIdentifier(selection.Effort) {
				continue
			}
			seen[selection] = true
			selections = append(selections, selection)
		}
	}
	if len(selections) == 0 || len(selections) > 128 {
		return Catalog{}, fmt.Errorf("radar catalog selection count is invalid")
	}
	sort.Slice(selections, func(i, j int) bool {
		if selections[i].Model != selections[j].Model {
			return selections[i].Model < selections[j].Model
		}
		return selections[i].Effort < selections[j].Effort
	})
	return Catalog{Benchmark: pool.Benchmark, ScoreVersion: pool.Version, TaskSetSHA256: pool.Hash, ScoreMax: pool.Max, RequiredTasks: pool.Tasks, Selections: selections}, nil
}

type metric struct {
	Mean         *float64 `json:"mean"`
	KnownTotal   *float64 `json:"known_total"`
	KnownSamples int      `json:"known_samples"`
}

func (m metric) average() *float64 {
	if m.KnownSamples <= 0 {
		return nil
	}
	if m.Mean != nil && finite(*m.Mean) && *m.Mean >= 0 {
		value := *m.Mean
		return &value
	}
	if m.KnownTotal != nil && finite(*m.KnownTotal) && *m.KnownTotal >= 0 {
		value := *m.KnownTotal / float64(m.KnownSamples)
		return &value
	}
	return nil
}

func (c *Client) Score(ctx context.Context, selection Selection, catalog Catalog) (Point, error) {
	query := url.Values{"model": {selection.Model}, "effort": {selection.Effort}}
	var raw struct {
		Model         string     `json:"model"`
		Effort        string     `json:"effort"`
		Benchmark     string     `json:"benchmark"`
		Version       string     `json:"score_version"`
		Hash          string     `json:"task_set_sha256"`
		Max           float64    `json:"score_max"`
		Score         *float64   `json:"score"`
		Coverage      int        `json:"coverage"`
		RequiredTasks int        `json:"required_tasks"`
		Status        string     `json:"score_status"`
		Events        int        `json:"valid_grade_events"`
		Latest        *time.Time `json:"latest_graded_at"`
		Trend         []struct {
			At    time.Time `json:"graded_at"`
			Score *float64  `json:"score"`
		} `json:"trend"`
		Statistics struct {
			Metrics map[string]metric `json:"metrics"`
		} `json:"statistics"`
	}
	if err := c.get(ctx, c.apiBase+"/api/v1/radar-bench-score?"+query.Encode(), &raw); err != nil {
		return Point{}, err
	}
	if raw.Model != selection.Model || raw.Effort != selection.Effort || raw.Benchmark != catalog.Benchmark || raw.Version != catalog.ScoreVersion || raw.Hash != catalog.TaskSetSHA256 || raw.Max != catalog.ScoreMax || raw.RequiredTasks != catalog.RequiredTasks {
		return Point{}, fmt.Errorf("radar score does not match the requested benchmark cohort")
	}
	if raw.Coverage < 0 || raw.Coverage > raw.RequiredTasks || raw.Events < 0 || !validScore(raw.Score, catalog.ScoreMax) {
		return Point{}, fmt.Errorf("radar score statistics are invalid")
	}
	cost, duration := raw.Statistics.Metrics["api_equivalent_usd"], raw.Statistics.Metrics["elapsed_ms"]
	minutes := duration.average()
	if minutes != nil {
		*minutes /= 60000
	}
	type daily struct {
		at    time.Time
		score float64
	}
	days := make(map[string]daily)
	for _, event := range raw.Trend {
		if event.Score == nil || !validScore(event.Score, catalog.ScoreMax) || event.At.IsZero() {
			continue
		}
		date := event.At.UTC().Format(time.DateOnly)
		if previous, exists := days[date]; !exists || event.At.After(previous.at) {
			days[date] = daily{event.At, *event.Score}
		}
	}
	history := make([]HistoryPoint, 0, len(days))
	for date, entry := range days {
		history = append(history, HistoryPoint{Date: date, Score: entry.score})
	}
	sort.Slice(history, func(i, j int) bool { return history[i].Date < history[j].Date })
	now := time.Now().UTC()
	return Point{Model: raw.Model, Effort: raw.Effort, Score: raw.Score, Coverage: raw.Coverage, RequiredTasks: raw.RequiredTasks, ScoreStatus: raw.Status, AveragePriceUSD: cost.average(), AverageMinutes: minutes, PriceSamples: max(0, cost.KnownSamples), DurationSamples: max(0, duration.KnownSamples), ValidGradeEvents: raw.Events, LatestGradedAt: raw.Latest, FetchedAt: &now, History: history}, nil
}

func (c *Client) Community(ctx context.Context) (Community, error) {
	var raw struct {
		OK        bool             `json:"ok"`
		Hours     int              `json:"window_hours"`
		UpdatedAt *time.Time       `json:"updated_at"`
		Models    []CommunityModel `json:"models"`
	}
	if err := c.get(ctx, c.webBase+"/api/model-ratings?view=public&window=24h", &raw); err != nil {
		return Community{}, err
	}
	if !raw.OK || raw.Hours != 24 || len(raw.Models) > 256 {
		return Community{}, fmt.Errorf("radar community window is invalid")
	}
	for _, model := range raw.Models {
		if model.ID == "" || model.Count < 0 || !validScore(model.Average, 10) || (model.Count == 0 && model.Average != nil) {
			return Community{}, fmt.Errorf("radar community rating is invalid")
		}
	}
	if raw.Models == nil {
		raw.Models = []CommunityModel{}
	}
	now := time.Now().UTC()
	return Community{WindowHours: 24, UpdatedAt: raw.UpdatedAt, FetchedAt: &now, Models: raw.Models}, nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func validScore(v *float64, maximum float64) bool {
	return v == nil || (finite(*v) && *v >= 0 && *v <= maximum)
}
func validIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' && char != '_' && char != '.' {
			return false
		}
	}
	return true
}

package iqradar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := NewClient(server.Client())
	client.apiBase, client.webBase = server.URL, server.URL
	return client
}

func writeJSON(w http.ResponseWriter, value any) { _ = json.NewEncoder(w).Encode(value) }

func TestCatalogDiscoversSelectionsWithoutRetiredModelsOrDuplicates(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/library-catalog" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		writeJSON(w, map[string]any{
			"retired_model_ids": []string{"old-model"},
			"unified_pool":      map[string]any{"benchmark": "bench", "score_version": "v150", "score_max": 150, "task_count": 64, "members_sha256": "cohort"},
			"collections": []any{map[string]any{"model_effort_selections": []any{
				map[string]string{"model": "gpt-6-astra", "effort": "xhigh"},
				map[string]string{"model": "gpt-6-astra", "effort": "xhigh"},
				map[string]string{"model": "old-model", "effort": "high"},
				map[string]string{"model": "gpt-6.1-sol", "effort": "low"},
			}}},
		})
	})
	catalog, err := client.Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Selections) != 2 || catalog.ScoreMax != 150 || catalog.RequiredTasks != 64 {
		t.Fatalf("unexpected catalog: %+v", catalog)
	}
}

func scoreFixture() map[string]any {
	return map[string]any{
		"model": "gpt-6-astra", "effort": "xhigh", "harness": "codex", "benchmark": "bench", "score_version": "v150", "score_max": 150,
		"task_set_sha256": "cohort", "score": 0, "coverage": 64, "required_tasks": 64, "score_status": "complete", "valid_grade_events": 8,
		"latest_graded_at": "2026-10-22T09:00:00Z",
		"statistics": map[string]any{"sample_count": 8, "metrics": map[string]any{
			"api_equivalent_usd": map[string]any{"mean": nil, "known_total": 12, "known_samples": 3},
			"elapsed_ms":         map[string]any{"mean": 120000, "known_total": 960000, "known_samples": 8},
		}},
		"trend": []any{
			map[string]any{"event_id": "late", "graded_at": "2026-10-20T23:00:00Z", "score": 30},
			map[string]any{"event_id": "early", "graded_at": "2026-10-20T01:00:00Z", "score": 20},
			map[string]any{"event_id": "tz", "graded_at": "2026-10-23T01:00:00+08:00", "score": 45},
			map[string]any{"event_id": "missing", "graded_at": "2026-10-24T00:00:00Z", "score": nil},
		},
	}
}

func fixtureCatalog() Catalog {
	return Catalog{Benchmark: "bench", ScoreVersion: "v150", ScoreMax: 150, RequiredTasks: 64, TaskSetSHA256: "cohort"}
}

func TestScorePreservesZeroUsesKnownSamplesAndLastGradePerUTCDay(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/radar-bench-score" || r.URL.Query().Get("model") != "gpt-6-astra" || r.URL.Query().Get("effort") != "xhigh" {
			t.Errorf("wrong source query %s", r.URL.String())
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("must not forward account credentials")
		}
		writeJSON(w, scoreFixture())
	})
	point, err := client.Score(context.Background(), Selection{Model: "gpt-6-astra", Effort: "xhigh"}, fixtureCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if point.Score == nil || *point.Score != 0 {
		t.Fatal("zero score lost")
	}
	if point.AveragePriceUSD == nil || *point.AveragePriceUSD != 4 || point.PriceSamples != 3 {
		t.Fatalf("wrong known cost average: %+v", point)
	}
	if point.AverageMinutes == nil || *point.AverageMinutes != 2 {
		t.Fatal("milliseconds must convert to minutes")
	}
	if len(point.History) != 2 || point.History[0].Date != "2026-10-20" || point.History[0].Score != 30 || point.History[1].Date != "2026-10-22" || point.History[1].Score != 45 {
		t.Fatalf("wrong UTC history: %+v", point.History)
	}
}

func TestScoreRejectsMixedModelOrBenchmarkCohorts(t *testing.T) {
	for _, key := range []string{"model", "effort", "benchmark", "score_version", "task_set_sha256"} {
		t.Run(key, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
				v := scoreFixture()
				v[key] = "different"
				writeJSON(w, v)
			})
			_, err := client.Score(context.Background(), Selection{Model: "gpt-6-astra", Effort: "xhigh"}, fixtureCatalog())
			if err == nil {
				t.Fatal("accepted mismatched source")
			}
		})
	}
}

func TestScoreUnknownCostIsNotFreeAndNullScoreIsNotZero(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		v := scoreFixture()
		v["score"] = nil
		v["statistics"] = map[string]any{"metrics": map[string]any{"api_equivalent_usd": map[string]any{"mean": nil, "known_total": 0, "known_samples": 0}}}
		writeJSON(w, v)
	})
	p, err := client.Score(context.Background(), Selection{Model: "gpt-6-astra", Effort: "xhigh"}, fixtureCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if p.Score != nil || p.AveragePriceUSD != nil || p.AverageMinutes != nil {
		t.Fatalf("missing values became numeric: %+v", p)
	}
}

func TestCommunityRequestsRolling24HoursAndPreservesUnratedModels(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/model-ratings" || r.URL.Query().Get("view") != "public" || r.URL.Query().Get("window") != "24h" {
			t.Errorf("wrong community query %s", r.URL.String())
		}
		writeJSON(w, map[string]any{"ok": true, "window_hours": 24, "updated_at": "2026-10-27T06:00:00Z", "models": []any{map[string]any{"id": "gpt-6-astra-low", "group": "GPT-6 Astra", "label": "GPT-6 Astra low", "count": 0, "average": nil}}})
	})
	community, err := client.Community(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if community.WindowHours != 24 || len(community.Models) != 1 || community.Models[0].Average != nil {
		t.Fatalf("wrong community: %+v", community)
	}
}

func TestSourceHTTPFailureAndRedirectAreNotAcceptedAsData(t *testing.T) {
	for _, status := range []int{http.StatusBadGateway, http.StatusFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", "https://example.invalid/")
				w.WriteHeader(status)
			})
			if _, err := client.Catalog(context.Background()); err == nil {
				t.Fatal("accepted failed response")
			}
		})
	}
}

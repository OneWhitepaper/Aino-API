package iqtest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func runnerFor(body, contentType string, status int) *Runner {
	return NewRunner(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}, nil)
}

func testInput(stream bool) RunInput {
	return RunInput{Connection: Connection{Source: "custom", BaseURL: "https://example.com/v1", APIKey: "test-secret"}, Kind: "candy", Model: "test-model", Stream: stream}
}

func TestRunnerJSONAndUnknownUsage(t *testing.T) {
	runner := runnerFor(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"最终答案：21"}]}]}`, "application/json", 200)
	result, err := runner.Run(context.Background(), testInput(false), nil)
	require.NoError(t, err)
	require.Equal(t, "最终答案：21", result.Output)
	require.Nil(t, result.Usage, "missing usage is unknown, not zero")
	runner = runnerFor(`{"status":"completed","output_text":"21","usage":{"input_tokens":0,"output_tokens":0}}`, "application/json", 200)
	result, err = runner.Run(context.Background(), testInput(false), nil)
	require.NoError(t, err)
	require.NotNil(t, result.Usage)
	require.EqualValues(t, 0, result.Usage.OutputTokens)
}

func TestRunnerSSERequiresCompletionAndReportsPartialOutput(t *testing.T) {
	body := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"最终答案：\"}\n\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"21\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":12,\"output_tokens\":5}}}\n\ndata: [DONE]\n\n"
	var partial string
	result, err := runnerFor(body, "text/event-stream", 200).Run(context.Background(), testInput(true), func(s string) { partial = s })
	require.NoError(t, err)
	require.Equal(t, "最终答案：21", partial)
	require.Equal(t, partial, result.Output)
	require.EqualValues(t, 12, result.Usage.InputTokens)
	_, err = runnerFor(strings.Split(body, "data: {\"type\":\"response.completed\"")[0], "text/event-stream", 200).Run(context.Background(), testInput(true), nil)
	require.EqualError(t, err, "incomplete_response")
}

func TestRunnerDoesNotExposeUpstreamErrorsOrGradeIncompleteAnswers(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
		code   string
	}{
		{`{"error":{"message":"test-secret invalid"}}`, 401, "upstream_auth"},
		{`{"status":"incomplete","output_text":"21"}`, 200, "incomplete_response"},
		{`{"status":"completed","output":[]}`, 200, "empty_response"},
		{`{"status":"completed","output_text":"21","usage":{"input_tokens":1}}`, 200, "invalid_response"},
	} {
		_, err := runnerFor(tc.body, "application/json", tc.status).Run(context.Background(), testInput(false), nil)
		require.EqualError(t, err, tc.code)
		require.NotContains(t, err.Error(), "test-secret")
	}
}

func TestRunnerPayloadAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "/v1/responses", r.URL.Path)
		require.Equal(t, "Bearer test-secret", r.Header.Get("Authorization"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, false, body["store"])
		require.NotContains(t, body, "tools")
		cancel()
		return nil, r.Context().Err()
	})}
	_, err := NewRunner(client, nil).Run(ctx, testInput(false), nil)
	require.True(t, errors.Is(err, context.Canceled))
}

func TestGradeUsesFinalAnswerAndKeepsVisualReviewHonest(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"先排除21。\n最终答案：22", "incorrect"},
		{"答案推导\n最终答案：21", "passed"},
		{"21", "passed"},
		{"121", "incorrect"},
		{"我考虑过 21 个，但还未得出结论", "ungraded"},
	} {
		verdict, _ := Grade("candy", tc.text)
		require.Equal(t, tc.want, verdict, tc.text)
	}
	verdict, _ := Grade("pelican", "<html><svg></svg></html>")
	require.Equal(t, "review", verdict)
}

func TestRunnerBoundsOutputAndDoesNotRetryBillableCalls(t *testing.T) {
	_, err := runnerFor(`{"status":"completed","output_text":"`+strings.Repeat("x", MaxOutputBytes+1)+`"}`, "application/json", 200).Run(context.Background(), testInput(false), nil)
	require.EqualError(t, err, "response_too_large")
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { calls++; return nil, errors.New("network test-secret") })}
	_, err = NewRunner(client, nil).Run(context.Background(), testInput(false), nil)
	require.EqualError(t, err, "connection_failed")
	require.Equal(t, 1, calls)
}

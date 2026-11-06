package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type syncValidationRepo struct {
	DesktopSyncRepository
	called bool
	input  DesktopSyncRequest
}

func (r *syncValidationRepo) Exchange(_ context.Context, _ int64, request DesktopSyncRequest, _ int64) (*DesktopSyncResponse, error) {
	r.called = true
	r.input = request
	return &DesktopSyncResponse{Cursor: "0", Results: []DesktopSyncResult{}, Changes: []DesktopSyncRecord{}}, nil
}

func syncValidRequest() DesktopSyncRequest {
	return DesktopSyncRequest{Version: 1, DeviceID: "installation-id", After: "0", Limit: 50, Operations: []DesktopSyncOperation{{OperationID: "00000000-0000-4000-8000-000000000001", Key: "conversation/default/id", BaseRevision: "0", Payload: json.RawMessage(`{"schema":1}`)}}}
}

func TestDesktopSyncRejectsMalformedInputBeforePublication(t *testing.T) {
	cases := map[string]func(*DesktopSyncRequest){
		"version":         func(r *DesktopSyncRequest) { r.Version = 2 },
		"device":          func(r *DesktopSyncRequest) { r.DeviceID = strings.Repeat("x", 257) },
		"cursor":          func(r *DesktopSyncRequest) { r.After = "-1" },
		"cursor overflow": func(r *DesktopSyncRequest) { r.After = "9223372036854775808" },
		"limit":           func(r *DesktopSyncRequest) { r.Limit = 51 },
		"operations":      func(r *DesktopSyncRequest) { r.Operations = make([]DesktopSyncOperation, 17) },
		"operation id":    func(r *DesktopSyncRequest) { r.Operations[0].OperationID = "bad" },
		"key":             func(r *DesktopSyncRequest) { r.Operations[0].Key = "a?b" },
		"revision":        func(r *DesktopSyncRequest) { r.Operations[0].BaseRevision = "01" },
		"document":        func(r *DesktopSyncRequest) { r.Operations[0].Payload = json.RawMessage(`[]`) },
		"invalid UTF-8": func(r *DesktopSyncRequest) {
			r.Operations[0].Payload = json.RawMessage([]byte{123, 34, 97, 34, 58, 34, 255, 34, 125})
		},
		"document bytes": func(r *DesktopSyncRequest) {
			r.Operations[0].Payload = json.RawMessage(`{"a":"` + strings.Repeat("x", 8*1024*1024) + `"}`)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := syncValidRequest()
			mutate(&r)
			repo := &syncValidationRepo{}
			_, err := NewDesktopSyncService(repo).Exchange(context.Background(), 1, r)
			require.ErrorIs(t, err, ErrDesktopSyncInvalid)
			require.False(t, repo.called)
		})
	}
}

func TestDesktopSyncCanonicalFingerprintPreservesExactJSONNumbers(t *testing.T) {
	a, b := syncValidRequest(), syncValidRequest()
	a.Operations[0].Payload = json.RawMessage(`{"n":9007199254740993,"schema":1}`)
	b.Operations[0].Payload = json.RawMessage(`{ "schema": 1, "n": 9007199254740993 }`)
	ra, rb := &syncValidationRepo{}, &syncValidationRepo{}
	_, err := NewDesktopSyncService(ra).Exchange(context.Background(), 1, a)
	require.NoError(t, err)
	_, err = NewDesktopSyncService(rb).Exchange(context.Background(), 1, b)
	require.NoError(t, err)
	require.Equal(t, ra.input.Operations[0].Fingerprint, rb.input.Operations[0].Fingerprint)
	require.Contains(t, string(ra.input.Operations[0].Payload), "9007199254740993")
	b.Operations[0].Payload = json.RawMessage(`{"n":9007199254740992,"schema":1}`)
	_, err = NewDesktopSyncService(rb).Exchange(context.Background(), 1, b)
	require.NoError(t, err)
	require.NotEqual(t, ra.input.Operations[0].Fingerprint, rb.input.Operations[0].Fingerprint)
}

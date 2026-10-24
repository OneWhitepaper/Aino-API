//go:build unit

package service

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func diagnosticFixture(t *testing.T, redacted bool, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := gzip.NewWriter(&buf)
	require.NoError(t, json.NewEncoder(z).Encode(map[string]any{
		"format": "hermes-debug-share/1", "redacted": redacted,
		"created": "2026-10-24T00:00:00Z", "files": files,
	}))
	require.NoError(t, z.Close())
	return buf.Bytes()
}

func TestDiagnosticBundleValidationDoesNotTrustRedactionOrCompression(t *testing.T) {
	valid := diagnosticFixture(t, true, map[string]string{"report": "synthetic", "client/desktop.log": "safe"})
	envelope, err := DecodeDiagnosticBundle(valid)
	require.NoError(t, err)
	require.Equal(t, "safe", envelope.Files["client/desktop.log"])
	for name, blob := range map[string][]byte{
		"unredacted":          diagnosticFixture(t, false, map[string]string{"report": "unsafe"}),
		"path traversal":      diagnosticFixture(t, true, map[string]string{"../private": "no"}),
		"missing report":      diagnosticFixture(t, true, map[string]string{"agent.log": "no"}),
		"invalid gzip":        []byte("not gzip"),
		"trailing gzip":       append(append([]byte{}, valid...), valid...),
		"decompression limit": diagnosticFixture(t, true, map[string]string{"report": strings.Repeat("x", MaxDiagnosticJSONBytes+1)}),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeDiagnosticBundle(blob)
			require.ErrorIs(t, err, ErrInvalidDiagnosticBundle)
		})
	}
	_, err = NewDiagnosticsService(nil).Upload(t.Context(), 1, base64.StdEncoding.EncodeToString([]byte("bad")))
	require.ErrorIs(t, err, ErrInvalidDiagnosticBundle, "validation must precede persistence")
}

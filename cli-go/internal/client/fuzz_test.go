package client

import (
	"net/http"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// Fuzz targets for code that parses untrusted responses. `go test` runs the
// seeds; `go test -run=^$ -fuzz=FuzzDecodeError ./internal/client` explores further.

// FuzzDecodeError checks that any error response becomes an APIError whose
// message is safe to print: valid UTF-8, no control characters (terminal
// escapes), and bounded in length.
func FuzzDecodeError(f *testing.F) {
	f.Add(403, "", []byte(`{"error":{"code":403,"message":"Quota exceeded.","errors":[{"domain":"global","reason":"quotaExceeded","message":"Quota exceeded."}]}}`))
	f.Add(429, "30", []byte(`{"error":{"code":429,"message":"Resource has been exhausted","status":"RESOURCE_EXHAUSTED"}}`))
	f.Add(403, "", []byte(`{"error":{"code":403,"message":"API has not been used","status":"PERMISSION_DENIED","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"SERVICE_DISABLED"}]}}`))
	f.Add(400, "", []byte(`{"error":{"message":"bad \u001b[31mred\u001b[0m","errors":[{"reason":"bad\u0007bell"}]}}`))
	f.Add(500, "", []byte("<html>Internal Server Error</html>"))
	f.Add(401, "", []byte(`{"error":null}`))
	f.Add(400, "", []byte(`{"error":{"message":"x`+strings.Repeat("é", 300)+`"}}`))
	f.Add(503, "120\x1b]0;title\x07", []byte(`{"error":{"status":"UNAVAILABLE\u001b[2J"}}`))
	f.Fuzz(func(t *testing.T, status int, retryAfter string, body []byte) {
		status = 100 + (status%500+500)%500
		h := http.Header{}
		h.Set("Retry-After", retryAfter)
		e := decodeError(&http.Response{StatusCode: status, Header: h}, body)
		if e.Status != status {
			t.Fatalf("status %d became %d", status, e.Status)
		}
		for _, field := range []string{e.Message, e.Reason, e.GoogleStatus, e.RetryAfter} {
			if !utf8.ValidString(field) || strings.ContainsFunc(field, unicode.IsControl) {
				t.Fatalf("unsafe text %q from body %q", field, body)
			}
		}
		if len(e.Message) > 503 || len(e.Error()) > 2048 {
			t.Fatalf("unbounded message (%d bytes) from body %q", len(e.Error()), body)
		}
	})
}

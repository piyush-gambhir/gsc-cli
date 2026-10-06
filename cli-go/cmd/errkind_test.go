package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/auth"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
)

func TestErrorKind(t *testing.T) {
	for want, err := range map[string]error{
		"usage":                 withKind(kindUsage, errors.New("x")),
		"auth":                  fmt.Errorf("refresh: %w", auth.ErrNoClient),
		"permission":            &client.APIError{Status: 403},
		"not_found":             fmt.Errorf("get: %w", &client.APIError{Status: 404}),
		"rate_limit":            &client.APIError{Status: 429},
		"server":                &client.APIError{Status: 503},
		"invalid_request":       &client.APIError{Status: 400},
		"timeout":               context.DeadlineExceeded,
		"network":               &url.Error{Op: "Get", URL: "https://x", Err: errors.New("connection refused")},
		"error":                 errors.New("something else"),
		"confirmation_required": withKind(kindConfirmation, errors.New("x")),
	} {
		if got := errorKind(err); got != want {
			t.Errorf("%v: got %s, want %s", err, got, want)
		}
	}
}

// The kind reaches the structured error for failures raised before and after flag parsing.
func TestErrorKindInOutput(t *testing.T) {
	isolate(t)
	f := newFake(t)
	kind := func(args ...string) string {
		r := cli(t, f, now, "", append(args, "-o", "json", "--no-input")...)
		var e map[string]any
		if r.code != 1 || json.Unmarshal([]byte(r.errOut), &e) != nil {
			t.Fatalf("%v: code=%d err=%q", args, r.code, r.errOut)
		}
		s, _ := e["kind"].(string)
		return s
	}
	for _, c := range []struct {
		want string
		args []string
	}{
		{"usage", []string{"qurey"}},
		{"usage", []string{"sites", "lsit"}},
		{"usage", []string{"query", "--limti", "5"}},
		{"usage", []string{"sites", "get", "a", "b"}},
		{"auth", []string{"sites", "list"}},
		{"auth", []string{"sites", "list", "--profile", "nope"}},
	} {
		if got := kind(c.args...); got != c.want {
			t.Errorf("%v: kind %q, want %q", c.args, got, c.want)
		}
	}
	t.Setenv("GSC_ACCESS_TOKEN", "env-token")
	for _, c := range []struct {
		want string
		args []string
	}{
		{"read_only", []string{"sites", "add", "sc-domain:x.com", "--read-only"}},
		{"confirmation_required", []string{"sites", "remove", "sc-domain:example.com"}},
		{"not_found", []string{"sites", "get", "sc-domain:missing.com"}},
	} {
		if got := kind(c.args...); got != c.want {
			t.Errorf("%v: kind %q, want %q", c.args, got, c.want)
		}
	}
}

// Google's legacy quota failures arrive as HTTP 403; they are rate limits, not permission problems.
func TestQuota403IsRateLimit(t *testing.T) {
	if got := errorKind(&client.APIError{Status: 403, Reason: "userRateLimitExceeded"}); got != "rate_limit" {
		t.Fatalf("got %s", got)
	}
	if got := errorKind(&client.APIError{Status: 403, Reason: "forbidden"}); got != "permission" {
		t.Fatalf("got %s", got)
	}
}

// Errors raised in command bodies carry their kind too.
func TestErrorKindFromCommandBodies(t *testing.T) {
	isolate(t)
	t.Setenv("GSC_ACCESS_TOKEN", "env-token")
	f := newFake(t)
	for _, c := range []struct {
		want string
		args []string
	}{
		{"read_only", []string{"api", "PUT", "/webmasters/v3/sites/sc-domain%3Ax.com", "--read-only"}},
		{"usage", []string{"api", "FETCH", "/x"}},
		{"usage", []string{"completion", "tcsh"}},
	} {
		r := cli(t, f, now, "", append(c.args, "-o", "json", "--no-input")...)
		var e map[string]any
		if r.code != 1 || json.Unmarshal([]byte(r.errOut), &e) != nil || e["kind"] != c.want {
			t.Errorf("%v: code=%d err=%q, want kind %s", c.args, r.code, r.errOut, c.want)
		}
	}
}

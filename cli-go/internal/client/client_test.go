package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type static string

func (s static) AccessToken(context.Context) (string, error) { return string(s), nil }

func respond(status int, body string, header ...string) *http.Response {
	h := http.Header{}
	for i := 0; i+1 < len(header); i += 2 {
		h.Set(header[i], header[i+1])
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body))}
}

func newTest(fn func(*http.Request) (*http.Response, error)) *Client {
	return New(&http.Client{Transport: roundTrip(fn), CheckRedirect: NoRedirects}, static("tok-123"))
}

func TestEscapingKeepsWholeIdentifierAsOneSegment(t *testing.T) {
	cases := map[string]string{
		"sc-domain:example.com":      "/webmasters/v3/sites/sc-domain%3Aexample.com/sitemaps/https%3A%2F%2Fexample.com%2Fsitemap.xml",
		"https://www.example.com/":   "/webmasters/v3/sites/https%3A%2F%2Fwww.example.com%2F/sitemaps/https%3A%2F%2Fexample.com%2Fsitemap.xml",
		"https://example.com/a%20b/": "/webmasters/v3/sites/https%3A%2F%2Fexample.com%2Fa%2520b%2F/sitemaps/https%3A%2F%2Fexample.com%2Fsitemap.xml",
	}
	for site, want := range cases {
		var got string
		c := newTest(func(r *http.Request) (*http.Response, error) {
			got = r.URL.EscapedPath()
			if r.Header.Get("Authorization") != "Bearer tok-123" {
				t.Fatal("missing bearer token")
			}
			return respond(200, `{}`), nil
		})
		if _, err := c.GetSitemap(context.Background(), site, "https://example.com/sitemap.xml"); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s:\n got  %s\n want %s", site, got, want)
		}
	}
}

func TestErrorEnvelopes(t *testing.T) {
	legacy := `{"error":{"code":403,"message":"Quota exceeded.","errors":[{"domain":"global","reason":"quotaExceeded","message":"Quota exceeded."}]}}`
	modern := `{"error":{"code":429,"message":"Resource has been exhausted","status":"RESOURCE_EXHAUSTED"}}`
	for _, tc := range []struct {
		status        int
		body, reason  string
		quota         bool
		retryAfterHdr string
	}{
		{403, legacy, "quotaExceeded", true, ""},
		{429, modern, "", true, "30"},
		{403, `{"error":{"code":403,"message":"User does not have sufficient permission for site 'x'."}}`, "", false, ""},
		{500, `<html>oops</html>`, "", false, ""},
	} {
		c := newTest(func(*http.Request) (*http.Response, error) {
			return respond(tc.status, tc.body, "Retry-After", tc.retryAfterHdr), nil
		})
		_, err := c.ListSites(context.Background())
		var api *APIError
		if !errors.As(err, &api) {
			t.Fatalf("not an APIError: %v", err)
		}
		if api.Status != tc.status || api.Reason != tc.reason || api.Quota() != tc.quota || api.RetryAfter != tc.retryAfterHdr {
			t.Fatalf("%+v", api)
		}
		if tc.quota && !strings.Contains(err.Error(), "No automatic retry") {
			t.Fatalf("quota hint missing: %v", err)
		}
		if strings.Contains(err.Error(), "<html>") {
			t.Fatal("reflected an HTML body")
		}
	}
}

func TestRedirectsAreNotFollowedAndTokenNotLeaked(t *testing.T) {
	calls := 0
	c := newTest(func(r *http.Request) (*http.Response, error) {
		calls++
		return respond(302, "", "Location", "https://evil.example/steal"), nil
	})
	_, err := c.ListSites(context.Background())
	if err == nil || calls != 1 {
		t.Fatalf("redirect followed: calls=%d err=%v", calls, err)
	}
	c = newTest(func(*http.Request) (*http.Response, error) { return nil, errors.New("dial failed for tok-123") })
	if _, err := c.ListSites(context.Background()); err == nil || strings.Contains(err.Error(), "tok-123") {
		t.Fatalf("token leaked: %v", err)
	}
}

func TestQueryDecodesMetadataBothSpellings(t *testing.T) {
	for body, wantHour := range map[string]string{
		`{"rows":[{"keys":["2026-09-30"],"clicks":3,"impressions":40,"ctr":0.075,"position":4.2}],"responseAggregationType":"byProperty","metadata":{"firstIncompleteDate":"2026-10-01","firstIncompleteHour":"2026-10-02T14:00:00-07:00"}}`: "2026-10-02T14:00:00-07:00",
		`{"rows":[],"metadata":{"first_incomplete_date":"2026-10-01","first_incomplete_hour":"2026-10-02T13:00:00-07:00"}}`:                                                                                                                  "2026-10-02T13:00:00-07:00",
	} {
		var sent QueryRequest
		c := newTest(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/searchAnalytics/query") {
				t.Fatalf("%s %s", r.Method, r.URL.Path)
			}
			_ = json.NewDecoder(r.Body).Decode(&sent)
			return respond(200, body), nil
		})
		resp, err := c.Query(context.Background(), "sc-domain:example.com", QueryRequest{StartDate: "2026-09-01", EndDate: "2026-09-30", Dimensions: []string{"date"}})
		if err != nil {
			t.Fatal(err)
		}
		if resp.Metadata == nil || resp.Metadata.FirstIncompleteDate != "2026-10-01" || resp.Metadata.FirstIncompleteHour != wantHour || sent.StartDate != "2026-09-01" {
			t.Fatalf("%+v %+v", resp.Metadata, sent)
		}
	}
}

func TestWritesAcceptEmptyBodies(t *testing.T) {
	var method string
	c := newTest(func(r *http.Request) (*http.Response, error) { method = r.Method; return respond(204, ""), nil })
	if err := c.SubmitSitemap(context.Background(), "sc-domain:example.com", "https://example.com/s.xml"); err != nil || method != http.MethodPut {
		t.Fatalf("%s %v", method, err)
	}
	if err := c.DeleteSite(context.Background(), "sc-domain:example.com"); err != nil || method != http.MethodDelete {
		t.Fatalf("%s %v", method, err)
	}
}

// Transport failures keep their cause for errors.Is/As (timeouts versus network
// errors) while the message stays free of the token.
func TestTransportErrorKeepsCause(t *testing.T) {
	c := newTest(func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })
	_, err := c.Do(context.Background(), http.MethodGet, "/webmasters/v3/sites?access_token=tok-123", nil, nil)
	var ue *url.Error
	if err == nil || !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &ue) || strings.Contains(err.Error(), "tok-123") {
		t.Fatalf("got %v", err)
	}
}

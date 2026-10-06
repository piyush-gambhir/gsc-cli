// Package client calls the Google Search Console API over plain HTTPS.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode"
)

const (
	DefaultBase = "https://searchconsole.googleapis.com"
	maxResponse = 32 << 20
)

// TokenSource returns a bearer access token for each request.
type TokenSource interface {
	AccessToken(ctx context.Context) (string, error)
}

type Client struct {
	HTTP *http.Client
	Base string
	Auth TokenSource
	// Log receives method, URL, and status lines under --verbose.
	Log       io.Writer
	UserAgent string
}

// NoRedirects is the CheckRedirect policy for every HTTP client that carries
// credentials: a redirect target never receives the Authorization header.
func NoRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func New(httpClient *http.Client, auth TokenSource) *Client {
	return &Client{HTTP: httpClient, Base: DefaultBase, Auth: auth, UserAgent: "gsc-cli"}
}

// APIError is a non-2xx response, decoded from Google's error envelope when present.
type APIError struct {
	Status       int    `json:"status"`
	GoogleStatus string `json:"google_status,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Message      string `json:"message"`
	RetryAfter   string `json:"retry_after,omitempty"`
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("Search Console API: %s (HTTP %d", e.Message, e.Status)
	if e.Reason != "" {
		msg += ", " + e.Reason
	}
	msg += ")"
	if e.RetryAfter != "" {
		msg += "; Retry-After: " + e.RetryAfter
	}
	if h := e.Hint(); h != "" {
		msg += ". " + h
	}
	return msg
}

// Quota reports whether the error is a rate or quota failure (403 or 429).
// requestError is a transport failure. Its message is scrubbed of the URL and
// token, and Unwrap keeps the cause so callers can tell timeouts from network errors.
type requestError struct {
	msg   string
	cause error
}

func (e *requestError) Error() string { return e.msg }
func (e *requestError) Unwrap() error { return e.cause }

func (e *APIError) Quota() bool {
	switch e.Reason {
	case "rateLimitExceeded", "userRateLimitExceeded", "quotaExceeded", "dailyLimitExceeded":
		return true
	}
	return e.Status == http.StatusTooManyRequests || e.GoogleStatus == "RESOURCE_EXHAUSTED"
}

// Hint explains what to do next for the failures users hit most.
func (e *APIError) Hint() string {
	switch {
	case e.Quota():
		return "A Search Console quota was exhausted. Search Analytics also has unpublished load quotas: narrow the date range, avoid grouping by page and query together, and wait about 15 minutes before retrying. No automatic retry was attempted"
	case e.Status == http.StatusUnauthorized:
		return "The access token was rejected; run gsc auth login"
	case e.Status == http.StatusForbidden && strings.Contains(strings.ToLower(e.Message), "permission"):
		return "The signed-in identity lacks access to this property; check gsc sites list or add the user in Search Console > Settings > Users and permissions"
	case e.Status == http.StatusForbidden && (e.Reason == "accessNotConfigured" || e.GoogleStatus == "PERMISSION_DENIED" && strings.Contains(e.Message, "has not been used")):
		return "Enable the Google Search Console API in the OAuth client's Google Cloud project"
	}
	return ""
}

type envelope struct {
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
		Errors  []struct {
			Reason  string `json:"reason"`
			Message string `json:"message"`
		} `json:"errors"`
		Details []struct {
			Type   string `json:"@type"`
			Reason string `json:"reason"`
		} `json:"details"`
	} `json:"error"`
}

func decodeError(res *http.Response, body []byte) *APIError {
	e := &APIError{Status: res.StatusCode, Message: http.StatusText(res.StatusCode), RetryAfter: sanitize(res.Header.Get("Retry-After"))}
	var env envelope
	if json.Unmarshal(body, &env) == nil && env.Error != nil {
		if env.Error.Message != "" {
			e.Message = sanitize(env.Error.Message)
		}
		e.GoogleStatus = sanitize(env.Error.Status)
		if len(env.Error.Errors) > 0 {
			e.Reason = sanitize(env.Error.Errors[0].Reason)
		}
		for _, d := range env.Error.Details {
			if e.Reason == "" && d.Reason != "" {
				e.Reason = sanitize(d.Reason)
			}
		}
	}
	return e
}

// sanitize makes response text safe to print: control characters (terminal
// escapes) become spaces, invalid UTF-8 is dropped, and the length is bounded
// without splitting a character.
func sanitize(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, ""))
	if len(s) > 500 {
		s = strings.ToValidUTF8(s[:500], "") + "..."
	}
	return s
}

// EscapeSegment percent-encodes everything except RFC 3986 unreserved
// characters, so a whole property URL or sitemap URL is one path segment.
func EscapeSegment(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// sensitiveParams are query parameters that can carry credentials.
var sensitiveParams = map[string]bool{
	"access_token": true, "oauth_token": true, "token": true, "id_token": true, "refresh_token": true,
	"key": true, "api_key": true, "apikey": true, "client_secret": true, "code": true, "assertion": true,
}

// SafeURL returns raw with credential-bearing query parameters masked, for
// logs, dry-run output, and errors. url.URL.Redacted only hides passwords.
func SafeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "[unparseable URL]"
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		// An unparseable query could hide a credential; mask all of it.
		u.RawQuery = "REDACTED"
		return u.Redacted()
	}
	masked := false
	for k := range q {
		if sensitiveParams[strings.ToLower(k)] {
			q.Set(k, "REDACTED")
			masked = true
		}
	}
	if masked {
		u.RawQuery = q.Encode()
	}
	return u.Redacted()
}

// Do sends one request and returns the response body. path must already be
// escaped. body, when non-nil, is sent as JSON.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		var b []byte
		switch v := body.(type) {
		case []byte:
			b = v
		case json.RawMessage:
			b = v
		default:
			var err error
			if b, err = json.Marshal(v); err != nil {
				return nil, err
			}
		}
		reader = bytes.NewReader(b)
	}
	target := strings.TrimRight(c.Base, "/") + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, errors.New(strings.ReplaceAll(err.Error(), target, SafeURL(target)))
	}
	if c.Auth == nil {
		return nil, errors.New("no credentials configured; run gsc auth login")
	}
	token, err := c.Auth.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", c.UserAgent)
	if c.Log != nil {
		fmt.Fprintf(c.Log, "%s %s\n", method, SafeURL(req.URL.String()))
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		msg := strings.ReplaceAll(err.Error(), target, SafeURL(target))
		return nil, &requestError{"request failed: " + strings.ReplaceAll(msg, token, "[REDACTED]"), err}
	}
	defer res.Body.Close()
	if c.Log != nil {
		fmt.Fprintf(c.Log, "HTTP %d\n", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if len(b) > maxResponse {
		return nil, fmt.Errorf("response exceeds 32 MiB")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, decodeError(res, b)
	}
	return b, nil
}

func (c *Client) getJSON(ctx context.Context, method, path string, query url.Values, body, out any) error {
	b, err := c.Do(ctx, method, path, query, body)
	if err != nil {
		return err
	}
	if out == nil || len(bytes.TrimSpace(b)) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("Search Console returned invalid JSON")
	}
	return nil
}

func sitePath(site string) string { return "/webmasters/v3/sites/" + EscapeSegment(site) }

// SiteEntry is one property and the caller's permission level on it.
type SiteEntry struct {
	SiteURL         string `json:"siteUrl"`
	PermissionLevel string `json:"permissionLevel"`
}

func (c *Client) ListSites(ctx context.Context) ([]SiteEntry, error) {
	var out struct {
		SiteEntry []SiteEntry `json:"siteEntry"`
	}
	if err := c.getJSON(ctx, http.MethodGet, "/webmasters/v3/sites", nil, nil, &out); err != nil {
		return nil, err
	}
	if out.SiteEntry == nil {
		out.SiteEntry = []SiteEntry{}
	}
	return out.SiteEntry, nil
}

func (c *Client) GetSite(ctx context.Context, site string) (SiteEntry, error) {
	var out SiteEntry
	err := c.getJSON(ctx, http.MethodGet, sitePath(site), nil, nil, &out)
	return out, err
}

func (c *Client) AddSite(ctx context.Context, site string) error {
	_, err := c.Do(ctx, http.MethodPut, sitePath(site), nil, nil)
	return err
}

func (c *Client) DeleteSite(ctx context.Context, site string) error {
	_, err := c.Do(ctx, http.MethodDelete, sitePath(site), nil, nil)
	return err
}

// ListSitemaps returns sitemaps as decoded JSON objects so new or legacy fields
// (and int64 counters encoded as strings) survive unchanged.
func (c *Client) ListSitemaps(ctx context.Context, site, sitemapIndex string) ([]map[string]any, error) {
	var q url.Values
	if sitemapIndex != "" {
		q = url.Values{"sitemapIndex": {sitemapIndex}}
	}
	var out struct {
		Sitemap []map[string]any `json:"sitemap"`
	}
	if err := c.getJSON(ctx, http.MethodGet, sitePath(site)+"/sitemaps", q, nil, &out); err != nil {
		return nil, err
	}
	if out.Sitemap == nil {
		out.Sitemap = []map[string]any{}
	}
	return out.Sitemap, nil
}

func (c *Client) GetSitemap(ctx context.Context, site, feed string) (map[string]any, error) {
	var out map[string]any
	err := c.getJSON(ctx, http.MethodGet, sitePath(site)+"/sitemaps/"+EscapeSegment(feed), nil, nil, &out)
	return out, err
}

func (c *Client) SubmitSitemap(ctx context.Context, site, feed string) error {
	_, err := c.Do(ctx, http.MethodPut, sitePath(site)+"/sitemaps/"+EscapeSegment(feed), nil, nil)
	return err
}

func (c *Client) DeleteSitemap(ctx context.Context, site, feed string) error {
	_, err := c.Do(ctx, http.MethodDelete, sitePath(site)+"/sitemaps/"+EscapeSegment(feed), nil, nil)
	return err
}

// Inspect calls URL Inspection and returns the decoded inspectionResult.
func (c *Client) Inspect(ctx context.Context, inspectionURL, site, languageCode string) (map[string]any, error) {
	body := map[string]string{"inspectionUrl": inspectionURL, "siteUrl": site}
	if languageCode != "" {
		body["languageCode"] = languageCode
	}
	var out struct {
		InspectionResult map[string]any `json:"inspectionResult"`
	}
	if err := c.getJSON(ctx, http.MethodPost, "/v1/urlInspection/index:inspect", nil, body, &out); err != nil {
		return nil, err
	}
	if out.InspectionResult == nil {
		return nil, fmt.Errorf("Search Console returned no inspectionResult")
	}
	return out.InspectionResult, nil
}

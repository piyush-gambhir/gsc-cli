package cmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/auth"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/secrets"
	"github.com/zalando/go-keyring"
)

func TestMain(m *testing.M) {
	keyring.MockInit()
	auth.BuiltinClientID, auth.BuiltinClientSecret = "test-client.apps.googleusercontent.com", "test-client-secret"
	os.Exit(m.Run())
}

// now is 2026-10-03 12:00 in Los Angeles.
var now = time.Date(2026, 10, 3, 19, 0, 0, 0, time.UTC)

func isolate(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	t.Setenv("GSC_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	for _, name := range []string{"GSC_ACCESS_TOKEN", "GSC_CREDENTIALS", "GSC_PROFILE", "GSC_SITE", "GSC_NO_INPUT", "GSC_QUIET", "GSC_VERBOSE", "GSC_READ_ONLY", "GSC_CLIENT_ID", "GSC_CLIENT_SECRET", "GOOGLE_APPLICATION_CREDENTIALS"} {
		t.Setenv(name, "")
	}
}

type call struct {
	Method, Path string
	Body         []byte
}

// fakeGoogle answers token, revoke, and Search Console requests in memory.
type fakeGoogle struct {
	t           *testing.T
	mu          sync.Mutex
	calls       []call
	sites       []client.SiteEntry
	refreshFail bool
	inspect     func(url string) (int, string)
	query       func(req client.QueryRequest) client.QueryResponse
}

func newFake(t *testing.T) *fakeGoogle {
	return &fakeGoogle{t: t, sites: []client.SiteEntry{{SiteURL: "sc-domain:example.com", PermissionLevel: "siteOwner"}}}
}

func (f *fakeGoogle) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c.Method+" "+c.Path, prefix) {
			n++
		}
	}
	return n
}

func (f *fakeGoogle) total() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.calls) }

func jsonResp(status int, v any) *http.Response {
	var b []byte
	switch x := v.(type) {
	case string:
		b = []byte(x)
	default:
		b, _ = json.Marshal(v)
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(b))}
}

func idToken() string {
	enc := base64.RawURLEncoding
	return enc.EncodeToString([]byte(`{}`)) + "." + enc.EncodeToString([]byte(`{"email":"me@example.com","sub":"sub-1"}`)) + ".x"
}

func (f *fakeGoogle) RoundTrip(r *http.Request) (*http.Response, error) {
	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(r.Body)
	}
	f.mu.Lock()
	f.calls = append(f.calls, call{r.Method, r.URL.Host + r.URL.EscapedPath(), body})
	f.mu.Unlock()
	switch r.URL.Host {
	case "oauth2.googleapis.com":
		form, _ := url.ParseQuery(string(body))
		if r.URL.Path == "/revoke" {
			return jsonResp(200, `{}`), nil
		}
		switch form.Get("grant_type") {
		case "authorization_code":
			if form.Get("code_verifier") == "" || form.Get("client_id") != auth.BuiltinClientID {
				return jsonResp(400, `{"error":"invalid_request"}`), nil
			}
			return jsonResp(200, map[string]any{"access_token": "at-1", "refresh_token": "rt-1", "expires_in": 3600, "token_type": "Bearer",
				"scope": "openid email " + auth.ScopeFull, "id_token": idToken()}), nil
		case "urn:ietf:params:oauth:grant-type:jwt-bearer":
			if form.Get("assertion") == "" {
				return jsonResp(400, `{"error":"invalid_grant"}`), nil
			}
			return jsonResp(200, map[string]any{"access_token": "sa-token", "expires_in": 3600, "token_type": "Bearer"}), nil
		case "refresh_token":
			if f.refreshFail {
				return jsonResp(400, `{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`), nil
			}
			return jsonResp(200, map[string]any{"access_token": "at-2", "expires_in": 3600, "token_type": "Bearer"}), nil
		}
		return jsonResp(400, `{"error":"unsupported_grant_type"}`), nil
	case "iamcredentials.googleapis.com":
		if r.Header.Get("Authorization") != "Bearer sa-token" {
			return jsonResp(403, `{"error":{"message":"denied"}}`), nil
		}
		return jsonResp(200, map[string]any{"accessToken": "impersonated-token", "expireTime": time.Now().Add(time.Hour).Format(time.RFC3339)}), nil
	case "searchconsole.googleapis.com":
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			return jsonResp(401, `{"error":{"code":401,"message":"no token"}}`), nil
		}
		p := r.URL.EscapedPath()
		switch {
		case r.Method == "GET" && p == "/webmasters/v3/sites":
			return jsonResp(200, map[string]any{"siteEntry": f.sites}), nil
		case strings.HasSuffix(p, "/searchAnalytics/query"):
			var req client.QueryRequest
			_ = json.Unmarshal(body, &req)
			var resp client.QueryResponse
			if f.query != nil {
				resp = f.query(req)
			} else {
				resp = defaultQuery(req)
			}
			// Like the real API, an offset past the end returns no rows.
			if req.StartRow >= len(resp.Rows) {
				resp.Rows = nil
			} else if req.StartRow > 0 {
				resp.Rows = resp.Rows[req.StartRow:]
			}
			return jsonResp(200, resp), nil
		case p == "/v1/urlInspection/index:inspect":
			var in map[string]string
			_ = json.Unmarshal(body, &in)
			status, resp := f.inspect(in["inspectionUrl"])
			return jsonResp(status, resp), nil
		case strings.Contains(p, "/sitemaps") && r.Method == "GET":
			return jsonResp(200, `{"sitemap":[{"path":"https://example.com/sitemap.xml","type":"sitemap","isPending":false,"errors":"0","warnings":"1"}]}`), nil
		case r.Method == "PUT" || r.Method == "DELETE":
			return jsonResp(200, ""), nil
		}
		return jsonResp(404, `{"error":{"code":404,"message":"not found"}}`), nil
	}
	f.t.Errorf("unexpected request to %s", r.URL)
	return nil, fmt.Errorf("unexpected host %s", r.URL.Host)
}

// defaultQuery returns one row per day for date probes and two rows otherwise.
func defaultQuery(req client.QueryRequest) client.QueryResponse {
	if len(req.Dimensions) == 1 && req.Dimensions[0] == "date" {
		var rows []client.Row
		start, _ := time.Parse("2006-01-02", req.StartDate)
		end, _ := time.Parse("2006-01-02", req.EndDate)
		for d := start; !d.After(end) && d.Before(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)); d = d.AddDate(0, 0, 1) {
			rows = append(rows, client.Row{Keys: []string{d.Format("2006-01-02")}, Clicks: 10, Impressions: 100, CTR: 0.1, Position: 5})
		}
		return client.QueryResponse{Rows: rows, Metadata: &client.Metadata{FirstIncompleteDate: "2026-10-01"}}
	}
	keys := func(i int) []string {
		k := make([]string, len(req.Dimensions))
		for j, d := range req.Dimensions {
			k[j] = fmt.Sprintf("%s-%d", d, i)
		}
		return k
	}
	return client.QueryResponse{Rows: []client.Row{{Keys: keys(1), Clicks: 20, Impressions: 400, CTR: 0.05, Position: 3.5}, {Keys: keys(2), Clicks: 5, Impressions: 250, CTR: 0.02, Position: 8}}, ResponseAggregationType: "byProperty"}
}

func browser(t *testing.T) func(string) error {
	return func(u string) error {
		q, _ := url.Parse(u)
		v := q.Query()
		go func() {
			res, err := http.Get(v.Get("redirect_uri") + "?code=c1&state=" + url.QueryEscape(v.Get("state")))
			if err == nil {
				res.Body.Close()
			}
		}()
		return nil
	}
}

type result struct {
	code        int
	out, errOut string
}

func cli(t *testing.T, f *fakeGoogle, at time.Time, stdin string, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	a := newApp(strings.NewReader(stdin), &out, &errb)
	a.transport = f
	a.openBrowser = browser(t)
	a.now = func() time.Time { return at }
	a.terminal = func() bool { return false }
	a.sleep = func(context.Context, time.Duration) error { return nil }
	code := a.run(context.Background(), args)
	return result{code, out.String(), errb.String()}
}

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("invalid JSON %q: %v", s, err)
	}
	return m
}

func TestLoginLifecycle(t *testing.T) {
	isolate(t)
	f := newFake(t)
	r := cli(t, f, now, "", "auth", "login", "-o", "json")
	if r.code != 0 {
		t.Fatalf("login: %s", r.errOut)
	}
	m := decode(t, r.out)
	if m["account"] != "me@example.com" || m["site"] != "sc-domain:example.com" || m["token_store"] != "keychain" || m["client"] != "builtin" {
		t.Fatalf("login output: %v", m)
	}
	if !strings.Contains(r.errOut, "Logged in as me@example.com") || strings.Contains(r.out+r.errOut, "rt-1") || strings.Contains(r.out+r.errOut, "at-1") {
		t.Fatalf("stderr/leak: %s", r.errOut)
	}
	if f.count("POST oauth2.googleapis.com/token") != 1 || f.count("GET searchconsole.googleapis.com/webmasters/v3/sites") != 1 {
		t.Fatalf("calls: %+v", f.calls)
	}
	before := f.total()
	r = cli(t, f, now, "", "auth", "status", "-o", "json")
	if r.code != 0 || f.total() != before {
		t.Fatalf("status must be local: %d %s", r.code, r.errOut)
	}
	if m := decode(t, r.out); m["credential_source"] != "profile:default" || m["write_access"] != true {
		t.Fatalf("%v", m)
	}
	r = cli(t, f, now, "", "sites", "list", "-o", "json")
	if r.code != 0 || f.count("POST oauth2.googleapis.com/token") != 1 {
		t.Fatalf("cached token not reused: %s", r.errOut)
	}
	expire(t, secrets.Keychain)
	r = cli(t, f, now, "", "sites", "list", "-o", "json")
	if r.code != 0 || f.count("POST oauth2.googleapis.com/token") != 2 {
		t.Fatalf("expired token not refreshed: %s", r.errOut)
	}
	before = f.total()
	r = cli(t, f, now, "", "sitemaps", "submit", "https://example.com/s.xml", "--read-only")
	if r.code == 0 || !strings.Contains(r.errOut, "--read-only") || f.total() != before {
		t.Fatalf("read-only write reached the network: %s", r.errOut)
	}
	r = cli(t, f, now, "", "sitemaps", "submit", "https://example.com/s.xml", "--dry-run", "-o", "json")
	if r.code != 0 || f.total() != before || !strings.Contains(r.out, "sc-domain%3Aexample.com/sitemaps/https%3A%2F%2Fexample.com%2Fs.xml") {
		t.Fatalf("dry-run: %s %s", r.out, r.errOut)
	}
	r = cli(t, f, now, "", "sitemaps", "submit", "https://example.com/s.xml", "-o", "json")
	if r.code != 0 || f.count("PUT searchconsole.googleapis.com/webmasters/v3/sites/sc-domain%3Aexample.com/sitemaps/") != 1 {
		t.Fatalf("submit: %s", r.errOut)
	}
	r = cli(t, f, now, "", "auth", "logout", "--revoke")
	if r.code == 0 || !strings.Contains(r.errOut, "--yes") || f.count("POST oauth2.googleapis.com/revoke") != 0 {
		t.Fatalf("revoke without confirmation: %s", r.errOut)
	}
	r = cli(t, f, now, "", "auth", "logout", "--revoke", "--yes", "-o", "json")
	if r.code != 0 || f.count("POST oauth2.googleapis.com/revoke") != 1 || decode(t, r.out)["revoked"] != true {
		t.Fatalf("logout: %s", r.errOut)
	}
	if _, err := keyring.Get(testService(t), "default"); err == nil {
		t.Fatal("token left in keychain after logout")
	}
	if r = cli(t, f, now, "", "auth", "status"); r.code == 0 {
		t.Fatal("status succeeded after logout")
	}
}

func TestLoginWhenKeychainUnavailableAndInvalidGrant(t *testing.T) {
	isolate(t)
	f := newFake(t)
	keyring.MockInitWithError(fmt.Errorf("org.freedesktop.secrets not provided"))
	r := cli(t, f, now, "", "auth", "login")
	if r.code == 0 || !strings.Contains(r.errOut, "--insecure-storage") {
		t.Fatalf("silent fallback or unclear error: %s", r.errOut)
	}
	r = cli(t, f, now, "", "auth", "login", "--insecure-storage", "-o", "json")
	if r.code != 0 || decode(t, r.out)["token_store"] != "file" || !strings.Contains(r.errOut, "plaintext") {
		t.Fatalf("insecure storage: %s %s", r.out, r.errOut)
	}
	f.refreshFail = true
	expire(t, secrets.File)
	r = cli(t, f, now, "", "sites", "list")
	if r.code == 0 || !strings.Contains(r.errOut, "gsc auth login --profile default") {
		t.Fatalf("invalid_grant guidance: %s", r.errOut)
	}
}

func TestLoginRefusesWithoutInteractionOrClient(t *testing.T) {
	isolate(t)
	f := newFake(t)
	if r := cli(t, f, now, "", "auth", "login", "--no-input"); r.code == 0 || !strings.Contains(r.errOut, "--service-account") || f.total() != 0 {
		t.Fatalf("no-input: %s", r.errOut)
	}
	id := auth.BuiltinClientID
	auth.BuiltinClientID = ""
	defer func() { auth.BuiltinClientID = id }()
	if r := cli(t, f, now, "", "login"); r.code == 0 || !strings.Contains(r.errOut, "GSC_CLIENT_ID") || f.total() != 0 {
		t.Fatalf("no client: %s", r.errOut)
	}
}

// expire marks the default profile's stored access token as expired.
func expire(t *testing.T, backend secrets.Backend) {
	t.Helper()
	store, err := (&app{}).store()
	if err != nil {
		t.Fatal(err)
	}
	b, err := auth.LoadBlob(store, backend, "default")
	if err != nil {
		t.Fatal(err)
	}
	b.Expiry = time.Now().Add(-time.Minute)
	if _, err := auth.SaveBlob(context.Background(), store, backend, "default", b); err != nil {
		t.Fatal(err)
	}
}

func lastBody(t *testing.T, f *fakeGoogle, suffix string) client.QueryRequest {
	return findBody(t, f, suffix, true)
}

// firstBody returns the first request to suffix (page one of a paged query).
func firstBody(t *testing.T, f *fakeGoogle, suffix string) client.QueryRequest {
	return findBody(t, f, suffix, false)
}

func findBody(t *testing.T, f *fakeGoogle, suffix string, last bool) client.QueryRequest {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for j := range f.calls {
		i := j
		if last {
			i = len(f.calls) - 1 - j
		}
		if strings.HasSuffix(f.calls[i].Path, suffix) {
			var req client.QueryRequest
			if err := json.Unmarshal(f.calls[i].Body, &req); err != nil {
				t.Fatal(err)
			}
			return req
		}
	}
	t.Fatalf("no request ending in %s", suffix)
	return client.QueryRequest{}
}

func TestQueryRequestBodyAndOutput(t *testing.T) {
	isolate(t)
	t.Setenv("GSC_ACCESS_TOKEN", "env-token")
	f := newFake(t)
	r := cli(t, f, now, "", "query", "-s", "sc-domain:example.com", "--start", "2026-09-01", "--end", "2026-09-30", "-d", "query,page",
		"--type", "web", "--filter", "country = ind", "--filter", "page =~ ^https://example.com/blog/", "--data-state", "ALL", "--aggregation", "bypage", "--limit", "10", "-o", "json")
	if r.code != 0 {
		t.Fatal(r.errOut)
	}
	got, _ := json.Marshal(firstBody(t, f, "/searchAnalytics/query"))
	want := `{"startDate":"2026-09-01","endDate":"2026-09-30","dimensions":["query","page"],"type":"web","dataState":"all","aggregationType":"byPage",` +
		`"dimensionFilterGroups":[{"groupType":"and","filters":[{"dimension":"country","operator":"equals","expression":"ind"},{"dimension":"page","operator":"includingRegex","expression":"^https://example.com/blog/"}]}],"rowLimit":10}`
	if string(got) != want {
		t.Fatalf("request body\n got  %s\n want %s", got, want)
	}
	m := decode(t, r.out)
	rows := m["rows"].([]any)
	if m["complete"] != true || len(rows) != 2 || rows[0].(map[string]any)["query"] != "query-1" || m["site"] != "sc-domain:example.com" {
		t.Fatalf("%v", m)
	}
	r = cli(t, f, now, "", "top", "queries", "-s", "sc-domain:example.com", "--start", "2026-09-01", "--end", "2026-09-30", "-o", "csv")
	recs, err := csv.NewReader(strings.NewReader(r.out)).ReadAll()
	if r.code != 0 || err != nil || strings.Join(recs[0], ",") != "query,clicks,impressions,ctr,position" || recs[1][3] != "0.05" {
		t.Fatalf("csv: %q %v %s", recs, err, r.errOut)
	}
	r = cli(t, f, now, "", "top", "queries", "-s", "sc-domain:example.com", "--start", "2026-09-01", "--end", "2026-09-30")
	if !strings.Contains(r.out, "5.00%") {
		t.Fatalf("table CTR not a percentage: %s", r.out)
	}
}

func TestLastProbesLatestDateAndCompare(t *testing.T) {
	isolate(t)
	t.Setenv("GSC_ACCESS_TOKEN", "env-token")
	f := newFake(t)
	r := cli(t, f, now, "", "query", "-s", "sc-domain:example.com", "--last", "7d", "-d", "query", "-o", "json")
	if r.code != 0 {
		t.Fatal(r.errOut)
	}
	req := lastBody(t, f, "/searchAnalytics/query")
	if req.StartDate != "2026-09-24" || req.EndDate != "2026-09-30" || !strings.Contains(r.errOut, "Latest final data: 2026-09-30") {
		t.Fatalf("--last anchored wrongly: %+v %s", req, r.errOut)
	}
	r = cli(t, f, now, "", "performance", "-s", "sc-domain:example.com", "--start", "2026-09-01", "--end", "2026-09-30", "--compare", "previous", "-o", "json")
	if r.code != 0 {
		t.Fatal(r.errOut)
	}
	prev := lastBody(t, f, "/searchAnalytics/query")
	if prev.StartDate != "2026-08-02" || prev.EndDate != "2026-08-31" || len(prev.Dimensions) != 0 {
		t.Fatalf("comparison period: %+v", prev)
	}
	m := decode(t, r.out)
	if m["mode"] != "previous" || m["current"].(map[string]any)["start"] != "2026-09-01" {
		t.Fatalf("%v", m)
	}
}

func TestTrendRollup(t *testing.T) {
	isolate(t)
	t.Setenv("GSC_ACCESS_TOKEN", "env-token")
	f := newFake(t)
	r := cli(t, f, now, "", "trend", "-s", "sc-domain:example.com", "--by", "week", "--start", "2026-09-14", "--end", "2026-09-27", "-o", "json")
	if r.code != 0 {
		t.Fatal(r.errOut)
	}
	m := decode(t, r.out)
	rows := m["rows"].([]any)
	if len(rows) != 2 || rows[0].(map[string]any)["week"] != "2026-09-14" || rows[0].(map[string]any)["clicks"] != 70.0 || m["rollup"] == nil {
		t.Fatalf("%v", m)
	}
}

func TestValidationBeforeNetwork(t *testing.T) {
	isolate(t)
	t.Setenv("GSC_ACCESS_TOKEN", "env-token")
	f := newFake(t)
	for _, args := range [][]string{
		{"query", "--start", "2026-09-30", "--end", "2026-09-01"},
		{"query", "-d", "hour", "--last", "2d"},
		{"query", "--filter", "date = 2026-01-01"},
		{"query", "--filter", "query =~ ("},
		{"query", "--aggregation", "byProperty", "-d", "page"},
		{"query", "--last", "5x"},
		{"query", "--raw", "--all"},
		{"query", "--compare", "lastweek"},
		{"query", "-o", "xml"},
		{"inspect", "not-a-url"},
		{"sites", "add", "example.com"},
		{"api", "GET", "https://evil.example/x"},
		{"trend", "--by", "day"},
	} {
		if r := cli(t, f, now, "", args...); r.code == 0 {
			t.Fatalf("accepted %v", args)
		}
	}
	if f.total() != 0 {
		t.Fatalf("validation errors reached the network: %+v", f.calls)
	}
	r := cli(t, f, now, "", "query", "-d", "query", "--last", "28d", "--print-request", "-o", "json")
	if r.code != 0 || f.total() != 0 || decode(t, r.out)["endDate"] != "2026-10-02" {
		t.Fatalf("print-request: %s %s", r.out, r.errOut)
	}
}

func TestInspectBatchStopsOnQuota(t *testing.T) {
	isolate(t)
	t.Setenv("GSC_ACCESS_TOKEN", "env-token")
	f := newFake(t)
	f.inspect = func(u string) (int, string) {
		switch {
		case strings.HasSuffix(u, "/ok"):
			return 200, `{"inspectionResult":{"indexStatusResult":{"verdict":"PASS","coverageState":"Submitted and indexed","googleCanonical":"https://example.com/ok"}}}`
		case strings.HasSuffix(u, "/bad"):
			return 400, `{"error":{"code":400,"message":"URL is not part of the property"}}`
		}
		return 429, `{"error":{"code":429,"message":"Quota exceeded","errors":[{"reason":"quotaExceeded"}]}}`
	}
	r := cli(t, f, now, "", "inspect", "-s", "sc-domain:example.com", "https://example.com/ok", "https://example.com/bad", "https://example.com/third", "https://example.com/never", "-o", "json")
	if r.code == 0 || !strings.Contains(r.errOut, "stopped after 1 of 4") {
		t.Fatalf("%d %s", r.code, r.errOut)
	}
	m := decode(t, r.out)
	if m["inspected"] != 1.0 || len(m["results"].([]any)) != 2 || m["stopped_reason"] == nil || f.count("POST searchconsole.googleapis.com/v1/urlInspection") != 3 {
		t.Fatalf("%v", m)
	}
	r = cli(t, f, now, "", "inspect", "-s", "sc-domain:example.com", "https://example.com/ok")
	if r.code != 0 || !strings.Contains(r.out, "Submitted and indexed") {
		t.Fatalf("table: %s %s", r.out, r.errOut)
	}
}

func TestAPICommand(t *testing.T) {
	isolate(t)
	t.Setenv("GSC_ACCESS_TOKEN", "env-token")
	f := newFake(t)
	if r := cli(t, f, now, "", "api", "GET", "/webmasters/v3/sites", "-o", "json"); r.code != 0 || !strings.Contains(r.out, "sc-domain:example.com") {
		t.Fatalf("%s %s", r.out, r.errOut)
	}
	before := f.total()
	if r := cli(t, f, now, "", "--read-only", "api", "PUT", "/webmasters/v3/sites/sc-domain%3Ax.com"); r.code == 0 || f.total() != before {
		t.Fatal("read-only api write was sent")
	}
	if r := cli(t, f, now, "", "--read-only", "api", "POST", "/webmasters/v3/sites/sc-domain%3Aexample.com/searchAnalytics/query", "--data", `{"startDate":"2026-09-01","endDate":"2026-09-02"}`); r.code != 0 {
		t.Fatalf("read-only read POST blocked: %s", r.errOut)
	}
	before = f.total()
	if r := cli(t, f, now, "", "api", "DELETE", "/webmasters/v3/sites/sc-domain%3Ax.com", "--dry-run", "-o", "json"); r.code != 0 || f.total() != before || !strings.Contains(r.out, `"dry_run": true`) {
		t.Fatalf("dry-run: %s", r.out)
	}
	before = f.total()
	if r := cli(t, f, now, "", "--no-input", "api", "DELETE", "/webmasters/v3/sites/sc-domain%3Ax.com"); r.code == 0 || f.total() != before || !strings.Contains(r.errOut, "--yes") {
		t.Fatalf("raw delete without --yes: code=%d calls=%d %s", r.code, f.total()-before, r.errOut)
	}
	if r := cli(t, f, now, "", "api", "PUT", "/webmasters/v3/sites/sc-domain%3Ax.com?access_token=review-sentinel", "--dry-run", "-o", "json"); r.code != 0 || strings.Contains(r.out+r.errOut, "review-sentinel") {
		t.Fatalf("dry-run printed a query credential: %s %s", r.out, r.errOut)
	}
	for path, read := range map[string]bool{"GET /x": true, "POST /webmasters/v3/sites/a/searchAnalytics/query": true, "POST /v1/urlInspection/index:inspect": true, "POST /v1/urlTestingTools/mobileFriendlyTest:run": false, "PUT /webmasters/v3/sites/a": false, "DELETE /x": false} {
		parts := strings.SplitN(path, " ", 2)
		if apiIsRead(parts[0], parts[1]) != read {
			t.Fatalf("%s classified wrongly", path)
		}
	}
}

func TestMachineErrorsAndOfflineCommands(t *testing.T) {
	isolate(t)
	f := newFake(t)
	r := cli(t, f, now, "", "sites", "list", "-o", "json")
	if r.code == 0 || r.out != "" || !json.Valid([]byte(r.errOut)) || !strings.Contains(r.errOut, "gsc auth login") {
		t.Fatalf("%d %q %q", r.code, r.out, r.errOut)
	}
	for _, args := range [][]string{{"version", "-o", "json"}, {"completion", "zsh"}, {"--help"}, {"auth", "list", "-o", "json"}, {"doctor", "-o", "json"}} {
		if r := cli(t, f, now, "", args...); r.code != 0 || r.out == "" {
			t.Fatalf("offline %v: %d %s", args, r.code, r.errOut)
		}
	}
	if f.total() != 0 {
		t.Fatalf("offline commands made requests: %+v", f.calls)
	}
}

func TestExportCommand(t *testing.T) {
	isolate(t)
	t.Setenv("GSC_ACCESS_TOKEN", "env-token")
	f := newFake(t)
	dir := t.TempDir()
	r := cli(t, f, now, "", "export", "-s", "sc-domain:example.com", "--start", "2026-09-01", "--end", "2026-09-02", "-d", "query", "--out", dir, "-o", "json")
	if r.code != 0 {
		t.Fatal(r.errOut)
	}
	if m := decode(t, r.out); m["days_written"] != 2.0 {
		t.Fatalf("%v", m)
	}
	for _, name := range []string{"2026-09-01.csv", "2026-09-02.csv", "manifest.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
}

// testService is the keychain service the CLI uses for the test's config path.
func testService(t *testing.T) string {
	t.Helper()
	st, err := (&app{}).store()
	if err != nil {
		t.Fatal(err)
	}
	return st.Service
}

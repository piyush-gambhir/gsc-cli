package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
)

func d(s string) Date {
	x, err := ParseDate(s)
	if err != nil {
		panic(err)
	}
	return x
}

func TestLastRangeAndMonthClamping(t *testing.T) {
	for _, tc := range []struct{ spec, end, start string }{
		{"28d", "2026-09-30", "2026-09-03"},
		{"1d", "2026-09-30", "2026-09-30"},
		{"4w", "2026-09-30", "2026-09-03"},
		{"3m", "2026-09-30", "2026-07-01"},
		{"1m", "2026-03-31", "2026-03-01"},
		{"16m", "2026-09-30", "2025-06-01"},
		{"1y", "2028-02-29", "2027-03-01"},
	} {
		r, err := LastRange(tc.spec, d(tc.end))
		if err != nil || r.Start.String() != tc.start || r.End.String() != tc.end {
			t.Fatalf("%s from %s: got %v..%v %v, want %s", tc.spec, tc.end, r.Start, r.End, err, tc.start)
		}
	}
	for _, bad := range []string{"", "0d", "5x", "d7", "-3d"} {
		if _, err := LastRange(bad, d("2026-09-30")); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestPacificDayBoundaryAndDST(t *testing.T) {
	// 06:30 UTC on Nov 1 2026 is still Oct 31 in Los Angeles (PDT, UTC-7).
	if got := DateOf(time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC)).String(); got != "2026-10-31" {
		t.Fatal(got)
	}
	// After the Nov 1 DST change (PST, UTC-8), 07:30 UTC on Nov 2 is Nov 1.
	if got := DateOf(time.Date(2026, 11, 2, 7, 30, 0, 0, time.UTC)).String(); got != "2026-11-01" {
		t.Fatal(got)
	}
	if r, _ := LastRange("7d", d("2026-11-03")); r.Days() != 7 {
		t.Fatal("DST week is not seven days", r.Days())
	}
}

func TestComparePeriodAndClip(t *testing.T) {
	r := Range{d("2026-09-01"), d("2026-09-30")}
	prev, _ := ComparePeriod(r, "previous")
	if prev.Start.String() != "2026-08-02" || prev.End.String() != "2026-08-31" || prev.Days() != 30 {
		t.Fatalf("%v..%v", prev.Start, prev.End)
	}
	yoy, _ := ComparePeriod(r, "yoy")
	if yoy.Start.String() != "2025-09-01" || yoy.End.String() != "2025-09-30" {
		t.Fatalf("%v..%v", yoy.Start, yoy.End)
	}
	if c, changed := Clip(Range{d("2025-01-01"), d("2025-02-01")}, d("2025-06-01")); !changed || c.Start.String() != "2025-06-01" {
		t.Fatalf("%v %v", c.Start, changed)
	}
}

func TestParseFilter(t *testing.T) {
	for in, want := range map[string]client.Filter{
		"query ~ running shoes":           {Dimension: "query", Operator: "contains", Expression: "running shoes"},
		"query !~ jobs":                   {Dimension: "query", Operator: "notContains", Expression: "jobs"},
		"page =~ ^https://example.com/b/": {Dimension: "page", Operator: "includingRegex", Expression: "^https://example.com/b/"},
		"page !=~ \\?":                    {Dimension: "page", Operator: "excludingRegex", Expression: "\\?"},
		"country=ind":                     {Dimension: "country", Operator: "equals", Expression: "ind"},
		"Device != MOBILE":                {Dimension: "device", Operator: "notEquals", Expression: "MOBILE"},
		`query = "a = b"`:                 {Dimension: "query", Operator: "equals", Expression: "a = b"},
		"searchappearance = AMP_ARTICLE":  {Dimension: "searchAppearance", Operator: "equals", Expression: "AMP_ARTICLE"},
	} {
		got, err := ParseFilter(in)
		if err != nil || got != want {
			t.Fatalf("%q: %+v %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "query", "date = 2026-09-01", "query >> x", "query =", "page =~ (", "hour = 1"} {
		if _, err := ParseFilter(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestNormalizeDocumentedRules(t *testing.T) {
	today := d("2026-10-03")
	base := func() client.QueryRequest { return client.QueryRequest{StartDate: "2026-09-01", EndDate: "2026-09-30"} }
	page := []client.FilterGroup{{Filters: []client.Filter{{Dimension: "page", Operator: "contains", Expression: "/a"}}}}
	showcase := []client.FilterGroup{{Filters: []client.Filter{{Dimension: "searchAppearance", Expression: "NEWS_SHOWCASE"}}}}
	bad := map[string]func(*client.QueryRequest){
		"start after end":         func(r *client.QueryRequest) { r.StartDate = "2026-10-01" },
		"bad date":                func(r *client.QueryRequest) { r.EndDate = "2026-9-30" },
		"duplicate":               func(r *client.QueryRequest) { r.Dimensions = []string{"query", "QUERY"} },
		"unknown dim":             func(r *client.QueryRequest) { r.Dimensions = []string{"browser"} },
		"hour needs hourly_all":   func(r *client.QueryRequest) { r.Dimensions = []string{"hour"} },
		"byProperty page":         func(r *client.QueryRequest) { r.AggregationType = "byProperty"; r.Dimensions = []string{"page"} },
		"byProperty page filter":  func(r *client.QueryRequest) { r.AggregationType = "byProperty"; r.DimensionFilterGroups = page },
		"byProperty discover":     func(r *client.QueryRequest) { r.AggregationType = "byproperty"; r.Type = "discover" },
		"showcase without filter": func(r *client.QueryRequest) { r.AggregationType = "byNewsShowcasePanel"; r.Type = "discover" },
		"showcase wrong type": func(r *client.QueryRequest) {
			r.AggregationType = "byNewsShowcasePanel"
			r.DimensionFilterGroups = showcase
		},
		"or group":     func(r *client.QueryRequest) { r.DimensionFilterGroups = []client.FilterGroup{{GroupType: "or"}} },
		"row limit":    func(r *client.QueryRequest) { r.RowLimit = 25001 },
		"unknown type": func(r *client.QueryRequest) { r.Type = "shopping" },
	}
	for name, mut := range bad {
		r := base()
		mut(&r)
		if _, err := Normalize(&r, today); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	ok := base()
	ok.Dimensions, ok.Type, ok.DataState, ok.AggregationType = []string{"QUERY", "page"}, "GOOGLENEWS", "ALL", "auto"
	if w, err := Normalize(&ok, today); err != nil || len(w) != 0 || ok.Dimensions[0] != "query" || ok.Type != "googleNews" || ok.DataState != "all" {
		t.Fatalf("%+v %v %v", ok, w, err)
	}
	sc := base()
	sc.AggregationType, sc.Type, sc.DimensionFilterGroups = "byNewsShowcasePanel", "discover", showcase
	if _, err := Normalize(&sc, today); err != nil {
		t.Fatal(err)
	}
	old := base()
	old.StartDate, old.Dimensions = "2024-01-01", []string{"searchAppearance", "query"}
	w, err := Normalize(&old, today)
	if err != nil || len(w) != 2 {
		t.Fatalf("retention and appearance should warn, not fail: %v %v", w, err)
	}
}

type fakeQuerier struct {
	total    int
	failAt   int // page index that fails, -1 never
	stuck    bool
	requests []client.QueryRequest
}

func (f *fakeQuerier) Query(_ context.Context, _ string, req client.QueryRequest) (*client.QueryResponse, error) {
	f.requests = append(f.requests, req)
	if f.failAt >= 0 && len(f.requests)-1 == f.failAt {
		return nil, errors.New("boom")
	}
	resp := &client.QueryResponse{ResponseAggregationType: "byProperty"}
	start := req.StartRow
	if f.stuck {
		start = 0
	}
	for i := start; i < f.total && i < start+req.RowLimit; i++ {
		resp.Rows = append(resp.Rows, client.Row{Keys: []string{fmt.Sprint("q", i)}, Clicks: float64(f.total - i), Impressions: 10})
	}
	return resp, nil
}

func TestFetchPagination(t *testing.T) {
	ctx := context.Background()
	req := client.QueryRequest{StartDate: "2026-09-01", EndDate: "2026-09-30", Dimensions: []string{"query"}}
	f := &fakeQuerier{total: 30000, failAt: -1}
	res, err := Fetch(ctx, f, "s", req, 0, true, nil)
	// Two data pages, then the empty page that proves the end (Google's extraction guide).
	if err != nil || len(res.Rows) != 30000 || !res.Complete || len(f.requests) != 3 {
		t.Fatalf("all: rows=%d complete=%v requests=%d %v", len(res.Rows), res.Complete, len(f.requests), err)
	}
	if f.requests[0].RowLimit != 25000 || f.requests[1].StartRow != 25000 {
		t.Fatalf("paging %+v", f.requests)
	}
	f = &fakeQuerier{total: 30000, failAt: -1}
	res, _ = Fetch(ctx, f, "s", req, 100, false, nil)
	if len(res.Rows) != 100 || res.Complete || f.requests[0].RowLimit != 100 {
		t.Fatalf("limit: %d %v", len(res.Rows), res.Complete)
	}
	f = &fakeQuerier{total: 7, failAt: -1}
	res, _ = Fetch(ctx, f, "s", req, 0, false, nil)
	if len(res.Rows) != 7 || !res.Complete || f.requests[0].RowLimit != DefaultLimit {
		t.Fatalf("default limit: %d %v", len(res.Rows), res.Complete)
	}
	if _, err := Fetch(ctx, &fakeQuerier{total: 60000, failAt: 1}, "s", req, 0, true, nil); err == nil || !strings.Contains(err.Error(), "nothing was printed") {
		t.Fatalf("partial failure: %v", err)
	}
	if _, err := Fetch(ctx, &fakeQuerier{total: 60000, failAt: -1, stuck: true}, "s", req, 0, true, nil); err == nil || !strings.Contains(err.Error(), "did not advance") {
		t.Fatalf("stuck pagination: %v", err)
	}
	// A short page followed by more exposed rows must not end the fetch early.
	short := &shortThenMore{}
	res, err = Fetch(ctx, short, "s", req, 0, true, nil)
	if err != nil || len(res.Rows) != 3 || !res.Complete {
		t.Fatalf("short page ended pagination: rows=%d complete=%v err=%v", len(res.Rows), res.Complete, err)
	}
}

// shortThenMore returns 2 rows, then 1 more row, then nothing.
type shortThenMore struct{ calls int }

func (s *shortThenMore) Query(_ context.Context, _ string, req client.QueryRequest) (*client.QueryResponse, error) {
	s.calls++
	resp := &client.QueryResponse{}
	switch s.calls {
	case 1:
		resp.Rows = []client.Row{{Keys: []string{"a"}}, {Keys: []string{"b"}}}
	case 2:
		resp.Rows = []client.Row{{Keys: []string{"c"}}}
	}
	return resp, nil
}

func TestResultJSONEnvelope(t *testing.T) {
	r := &Result{Site: "sc-domain:example.com", Request: client.QueryRequest{StartDate: "2026-09-01", EndDate: "2026-09-30", Dimensions: []string{"query", "page"}},
		Complete: true, Requests: 1, Rows: []client.Row{{Keys: []string{"shoes", "https://e.com/a"}, Clicks: 3, Impressions: 40, CTR: 0.075, Position: 4.2}}}
	b, _ := json.Marshal(r)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["metadata_returned"] != false || m["first_incomplete_hour"] != nil || m["type"] != "web" || m["data_state"] != "final" || m["row_count"] != float64(1) {
		t.Fatalf("%s", b)
	}
	row := m["rows"].([]any)[0].(map[string]any)
	if row["query"] != "shoes" || row["page"] != "https://e.com/a" || row["ctr"] != 0.075 {
		t.Fatalf("%v", row)
	}
	if !strings.HasPrefix(string(b), `{"site":`) || strings.Index(string(b), `"query":"shoes"`) > strings.Index(string(b), `"clicks":3`) {
		t.Fatalf("field order: %s", b)
	}
}

func TestCompareUsesNullForUnobserved(t *testing.T) {
	req := client.QueryRequest{Dimensions: []string{"query"}}
	cur := &Result{Request: req, Rows: []client.Row{{Keys: []string{"a"}, Clicks: 10, Impressions: 100, CTR: 0.10, Position: 3}, {Keys: []string{"new"}, Clicks: 1, Impressions: 5, CTR: 0.2, Position: 9}}}
	prev := &Result{Request: req, Rows: []client.Row{{Keys: []string{"a"}, Clicks: 0, Impressions: 50, CTR: 0.08, Position: 5}, {Keys: []string{"gone"}, Clicks: 4, Impressions: 40, CTR: 0.1, Position: 6}}}
	c := Compare("previous", cur, prev)
	if len(c.Rows) != 3 || c.Rows[2].Keys[0] != "gone" {
		t.Fatalf("%+v", c.Rows)
	}
	v := c.Rows[0].Values()
	if v[2] != 10.0 || v[3] != nil || math.Abs(v[10].(float64)-2.0) > 1e-9 || v[13] != -2.0 {
		t.Fatalf("both observed: %v", v)
	}
	if v := c.Rows[1].Values(); v[1] != nil || v[2] != nil || v[3] != nil {
		t.Fatalf("new row must be null on the previous side: %v", v)
	}
	b, _ := json.Marshal(c)
	if !strings.Contains(string(b), `"clicks_prev":null`) || !strings.Contains(string(b), `"note"`) {
		t.Fatalf("%s", b)
	}
	if got := Exposure(c, "new", 0); len(got.Rows) != 1 || got.Rows[0][0] != "new" {
		t.Fatalf("new: %v", got.Rows)
	}
	if got := Exposure(c, "lost", 0); len(got.Rows) != 1 || got.Rows[0][0] != "gone" {
		t.Fatalf("lost: %v", got.Rows)
	}
	if got := Decliners(c, 0, 0); len(got.Rows) != 0 {
		t.Fatalf("decliners: a gained clicks: %v", got.Rows)
	}
}

func TestRollupMath(t *testing.T) {
	rows := []client.Row{
		{Keys: []string{"2026-09-14"}, Clicks: 10, Impressions: 100, Position: 2}, // Monday
		{Keys: []string{"2026-09-20"}, Clicks: 0, Impressions: 300, Position: 10}, // Sunday, same ISO week
		{Keys: []string{"2026-09-21"}, Clicks: 5, Impressions: 50, Position: 4},   // next Monday
	}
	weeks, err := Rollup(rows, "week")
	if err != nil || len(weeks) != 2 || weeks[0].Keys[0] != "2026-09-14" {
		t.Fatalf("%+v %v", weeks, err)
	}
	if weeks[0].Clicks != 10 || weeks[0].Impressions != 400 || weeks[0].CTR != 0.025 || weeks[0].Position != 8 {
		t.Fatalf("weighted: %+v", weeks[0])
	}
	months, _ := Rollup(rows, "month")
	if len(months) != 1 || months[0].Keys[0] != "2026-09" || months[0].Impressions != 450 {
		t.Fatalf("%+v", months)
	}
	if _, err := Rollup([]client.Row{{Keys: []string{"a", "b"}}}, "week"); err == nil {
		t.Fatal("accepted multi-dimension rows")
	}
}

func TestInsights(t *testing.T) {
	q := &Result{Request: client.QueryRequest{Dimensions: []string{"query"}}, Rows: []client.Row{
		{Keys: []string{"close"}, Clicks: 5, Impressions: 900, CTR: 0.005, Position: 6},
		{Keys: []string{"top"}, Clicks: 300, Impressions: 1000, CTR: 0.3, Position: 1.2},
		{Keys: []string{"far"}, Clicks: 0, Impressions: 500, Position: 45},
		{Keys: []string{"b1"}, Clicks: 50, Impressions: 500, CTR: 0.1, Position: 6.2},
		{Keys: []string{"b2"}, Clicks: 40, Impressions: 500, CTR: 0.08, Position: 5.8},
	}}
	sd := StrikingDistance(q, 4, 20, 100, 0)
	if len(sd.Rows) != 3 || sd.Rows[0][0] != "close" {
		t.Fatalf("striking: %v", sd.Rows)
	}
	low := LowCTR(q, 100, 0.5, 0)
	if len(low.Rows) != 1 || low.Rows[0][0] != "close" || low.Rows[0][6] != 0.08 {
		t.Fatalf("low-ctr: %v", low.Rows)
	}
	pages := &Result{Request: client.QueryRequest{Dimensions: []string{"query", "page"}}, Rows: []client.Row{
		{Keys: []string{"shoes", "/a"}, Impressions: 600}, {Keys: []string{"shoes", "/b"}, Impressions: 400}, {Keys: []string{"shoes", "/c"}, Impressions: 5},
		{Keys: []string{"hats", "/h"}, Impressions: 900},
	}}
	cn := Cannibalization(pages, 0.1, 100, 0)
	if len(cn.Rows) != 2 || cn.Rows[0][1] != "/a" || math.Abs(cn.Rows[1][4].(float64)-400.0/1005) > 1e-9 {
		t.Fatalf("cannibalization: %v", cn.Rows)
	}
	b, _ := json.Marshal(cn)
	if !strings.Contains(string(b), `"thresholds":{"min_page_share":0.1`) {
		t.Fatalf("%s", b)
	}
}

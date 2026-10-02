package export

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/analytics"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
)

type dayQuerier struct {
	failDay        string
	failTimes      int
	incomplete     string
	incompleteHour string
	empty          bool
	requests       []client.QueryRequest
}

func (q *dayQuerier) Query(_ context.Context, _ string, req client.QueryRequest) (*client.QueryResponse, error) {
	q.requests = append(q.requests, req)
	if req.StartDate == q.failDay && q.failTimes > 0 {
		q.failTimes--
		return nil, &client.APIError{Status: 429, Message: "Quota exceeded", Reason: "rateLimitExceeded"}
	}
	resp := &client.QueryResponse{}
	// One row per day; like the real API, an offset past it returns nothing.
	if req.StartRow == 0 && !q.empty {
		resp.Rows = []client.Row{{Keys: []string{req.StartDate, "=cmd"}, Clicks: 1, Impressions: 2, CTR: 0.5, Position: 3}}
	}
	if q.incomplete != "" || q.incompleteHour != "" {
		resp.Metadata = &client.Metadata{FirstIncompleteDate: q.incomplete, FirstIncompleteHour: q.incompleteHour}
	}
	return resp, nil
}

func rng(a, b string) analytics.Range {
	s, _ := analytics.ParseDate(a)
	e, _ := analytics.ParseDate(b)
	return analytics.Range{Start: s, End: e}
}

func noSleep(context.Context, time.Duration) error { return nil }

func TestExportResumeAndAtomicDays(t *testing.T) {
	dir := t.TempDir()
	base := client.QueryRequest{Dimensions: []string{"query"}, DataState: "final"}
	q := &dayQuerier{failDay: "2026-09-02", failTimes: 1}
	opts := Options{Site: "sc-domain:example.com", Base: base, Range: rng("2026-09-01", "2026-09-03"), Format: "csv", OutDir: dir, Sleep: noSleep}
	sum, err := Run(context.Background(), q, opts)
	if err == nil || !strings.Contains(err.Error(), "rerun the same command to resume") || sum.Written != 1 {
		t.Fatalf("expected stop after day 1: %+v %v", sum, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-09-02.csv")); !os.IsNotExist(err) {
		t.Fatal("failed day left a file")
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".export-*"))
	if len(leftovers) != 0 {
		t.Fatal("temp files left behind", leftovers)
	}
	q.requests = nil
	sum, err = Run(context.Background(), q, opts)
	// Two days, each a data page plus the empty page that proves its end.
	if err != nil || sum.Skipped != 1 || sum.Written != 2 || len(q.requests) != 4 {
		t.Fatalf("resume: %+v %v requests=%d", sum, err, len(q.requests))
	}
	if q.requests[0].Dimensions[0] != "date" || q.requests[0].StartDate != q.requests[0].EndDate {
		t.Fatalf("per-day request: %+v", q.requests[0])
	}
	f, _ := os.Open(filepath.Join(dir, "2026-09-03.csv"))
	recs, _ := csv.NewReader(f).ReadAll()
	f.Close()
	if recs[0][0] != "date" || recs[0][1] != "query" || recs[1][1] != "'=cmd" {
		t.Fatalf("csv: %q", recs)
	}
	opts.Base.Dimensions = []string{"page"}
	if _, err := Run(context.Background(), q, opts); err == nil || !strings.Contains(err.Error(), "different request") {
		t.Fatalf("manifest mismatch accepted: %v", err)
	}
	opts.Restart = true
	if sum, err := Run(context.Background(), q, opts); err != nil || sum.Written != 3 {
		t.Fatalf("restart: %+v %v", sum, err)
	}
}

func TestExportRetryAndPreliminaryDays(t *testing.T) {
	dir := t.TempDir()
	q := &dayQuerier{failDay: "2026-09-29", failTimes: 2, incomplete: "2026-09-30"}
	opts := Options{Site: "s", Base: client.QueryRequest{DataState: "all"}, Range: rng("2026-09-29", "2026-09-30"), Format: "ndjson", OutDir: dir, Retry: 2, Sleep: noSleep}
	sum, err := Run(context.Background(), q, opts)
	if err != nil || sum.Written != 2 || len(sum.PreliminaryDays) != 1 || sum.PreliminaryDays[0] != "2026-09-30" {
		t.Fatalf("%+v %v", sum, err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "2026-09-30.ndjson"))
	var row map[string]any
	if err := json.Unmarshal([]byte(strings.Split(string(b), "\n")[0]), &row); err != nil || row["date"] != "2026-09-30" {
		t.Fatalf("ndjson: %s %v", b, err)
	}
	q.requests = nil
	sum, _ = Run(context.Background(), q, opts)
	if sum.Skipped != 1 || sum.Written != 1 || q.requests[0].StartDate != "2026-09-30" {
		t.Fatalf("preliminary day not refetched: %+v", sum)
	}
	q2 := &dayQuerier{failDay: "2026-09-29", failTimes: 5}
	if _, err := Run(context.Background(), q2, Options{Site: "s", Range: rng("2026-09-29", "2026-09-29"), Format: "csv", OutDir: t.TempDir(), Sleep: noSleep}); err == nil {
		t.Fatal("retried without --retry")
	} else if !errors.As(err, new(*client.APIError)) {
		t.Fatalf("lost the API error: %v", err)
	}
	if len(q2.requests) != 1 {
		t.Fatalf("automatic retry happened: %d requests", len(q2.requests))
	}
}

func TestExportFinalityUsesHourMarkerAndFinalDate(t *testing.T) {
	// Hourly export: only the hour marker says which days are still changing.
	dir := t.TempDir()
	q := &dayQuerier{incompleteHour: "2026-09-30T13:00:00-07:00"}
	opts := Options{Site: "s", Base: client.QueryRequest{Dimensions: []string{"hour"}, DataState: "hourly_all"}, Range: rng("2026-09-29", "2026-09-30"),
		Format: "ndjson", OutDir: dir, Sleep: noSleep}
	sum, err := Run(context.Background(), q, opts)
	if err != nil || len(sum.PreliminaryDays) != 1 || sum.PreliminaryDays[0] != "2026-09-30" {
		t.Fatalf("hour marker ignored: %+v %v", sum, err)
	}
	// Final data state: days after the latest final date stay pending even when empty.
	dir = t.TempDir()
	final, _ := analytics.ParseDate("2026-09-29")
	q = &dayQuerier{empty: true}
	opts = Options{Site: "s", Base: client.QueryRequest{DataState: "final"}, Range: rng("2026-09-29", "2026-10-02"), Format: "csv", OutDir: dir,
		Sleep: noSleep, FinalThrough: final}
	sum, err = Run(context.Background(), q, opts)
	if err != nil || len(sum.PreliminaryDays) != 3 {
		t.Fatalf("days after the final date must stay pending: %+v %v", sum, err)
	}
	q.requests = nil
	if sum, err = Run(context.Background(), q, opts); err != nil || sum.Skipped != 1 || sum.Written != 3 {
		t.Fatalf("resume must re-fetch pending days: %+v %v", sum, err)
	}
}

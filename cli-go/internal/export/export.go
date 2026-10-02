// Package export writes Search Analytics data one day per file, resumably.
package export

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/analytics"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/output"
)

const manifestName = "manifest.json"

type Options struct {
	Site   string
	Base   client.QueryRequest // dimensions, type, filters, data state, aggregation
	Range  analytics.Range
	Format string // csv or ndjson
	OutDir string
	// Retry is the number of retries for quota and 5xx errors (default 0).
	Retry int
	// MinInterval spaces requests to stay under per-site QPM limits.
	MinInterval time.Duration
	// Restart discards a manifest written for a different request.
	Restart bool
	// FinalThrough is the latest date with final data. Later days (including
	// empty and future ones) are recorded as preliminary and re-fetched on resume.
	FinalThrough analytics.Date
	Log          io.Writer
	Sleep        func(context.Context, time.Duration) error
}

type Manifest struct {
	Version     int                  `json:"version"`
	RequestHash string               `json:"request_hash"`
	Site        string               `json:"site"`
	Request     client.QueryRequest  `json:"request"`
	Format      string               `json:"format"`
	Days        map[string]DayRecord `json:"days"`
}

type DayRecord struct {
	File        string `json:"file"`
	Rows        int    `json:"rows"`
	Complete    bool   `json:"complete"`
	Preliminary bool   `json:"preliminary"`
	Requests    int    `json:"requests"`
}

type Summary struct {
	OutDir          string   `json:"out_dir"`
	Manifest        string   `json:"manifest"`
	Days            int      `json:"days"`
	Written         int      `json:"days_written"`
	Skipped         int      `json:"days_skipped_already_final"`
	Rows            int      `json:"rows_written"`
	Requests        int      `json:"requests"`
	PreliminaryDays []string `json:"preliminary_days"`
	IncompleteDays  []string `json:"days_with_pagination_cut"`
}

// requestFor is the per-day request. A "date" dimension is added first (it is
// a no-op for a single day) so every row carries its date and Google returns
// incompleteness metadata for preliminary data.
func requestFor(base client.QueryRequest, day string) client.QueryRequest {
	req := base
	req.Dimensions = slices.Clone(base.Dimensions)
	if !slices.Contains(req.Dimensions, "date") && !slices.Contains(req.Dimensions, "hour") {
		req.Dimensions = append([]string{"date"}, req.Dimensions...)
	}
	req.StartDate, req.EndDate, req.StartRow, req.RowLimit = day, day, 0, 0
	return req
}

func hashRequest(site, format string, base client.QueryRequest) string {
	b, _ := json.Marshal(struct {
		Site    string
		Format  string
		Request client.QueryRequest
	}{site, format, base})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func Run(ctx context.Context, q analytics.Querier, o Options) (*Summary, error) {
	if o.Format != "csv" && o.Format != "ndjson" {
		return nil, fmt.Errorf("--format must be csv or ndjson")
	}
	if o.Sleep == nil {
		o.Sleep = sleep
	}
	if o.Log == nil {
		o.Log = io.Discard
	}
	if err := os.MkdirAll(o.OutDir, 0o755); err != nil {
		return nil, err
	}
	base := o.Base
	base.StartDate, base.EndDate, base.StartRow, base.RowLimit = "", "", 0, 0
	hash := hashRequest(o.Site, o.Format, base)
	mpath := filepath.Join(o.OutDir, manifestName)
	m, err := loadManifest(mpath)
	if err != nil {
		return nil, err
	}
	if m != nil && m.RequestHash != hash {
		if !o.Restart {
			return nil, fmt.Errorf("%s was written for a different request (site, dimensions, filters, data state, or format); use a new --out directory or pass --restart", mpath)
		}
		m = nil
	}
	if m == nil {
		m = &Manifest{Version: 1, RequestHash: hash, Site: o.Site, Request: base, Format: o.Format, Days: map[string]DayRecord{}}
	}
	sum := &Summary{OutDir: o.OutDir, Manifest: mpath, PreliminaryDays: []string{}, IncompleteDays: []string{}}
	var last time.Time
	for d := o.Range.Start; !d.After(o.Range.End); d = d.AddDays(1) {
		day := d.String()
		sum.Days++
		// Skip days already recorded as final, unless they are newer than the latest
		// final date (a record written before that date was known is re-checked).
		if rec, ok := m.Days[day]; ok && !rec.Preliminary && (o.FinalThrough.IsZero() || !d.After(o.FinalThrough)) {
			sum.Skipped++
			continue
		}
		req := requestFor(base, day)
		var res *analytics.Result
		for attempt := 0; ; attempt++ {
			if wait := o.MinInterval - time.Since(last); !last.IsZero() && wait > 0 {
				if err := o.Sleep(ctx, wait); err != nil {
					return sum, err
				}
			}
			last = time.Now()
			res, err = analytics.Fetch(ctx, q, o.Site, req, 0, true, nil)
			if res != nil {
				sum.Requests += res.Requests
			} else {
				sum.Requests++
			}
			if err == nil {
				break
			}
			delay, retryable := retryDelay(err, attempt)
			if !retryable || attempt >= o.Retry {
				return sum, fmt.Errorf("export stopped at %s (earlier days are saved; rerun the same command to resume): %w", day, err)
			}
			fmt.Fprintf(o.Log, "Retrying %s in %s after: %v\n", day, delay, err)
			if err := o.Sleep(ctx, delay); err != nil {
				return sum, err
			}
		}
		file := day + "." + o.Format
		if err := writeDay(filepath.Join(o.OutDir, file), o.Format, res); err != nil {
			return sum, err
		}
		prelim := preliminary(d, req.DataState, res, o.FinalThrough)
		m.Days[day] = DayRecord{File: file, Rows: len(res.Rows), Complete: res.Complete, Preliminary: prelim, Requests: res.Requests}
		if err := saveManifest(mpath, m); err != nil {
			return sum, err
		}
		sum.Written++
		sum.Rows += len(res.Rows)
		if prelim {
			sum.PreliminaryDays = append(sum.PreliminaryDays, day)
		}
		if !res.Complete {
			sum.IncompleteDays = append(sum.IncompleteDays, day)
		}
		fmt.Fprintf(o.Log, "%s: %d rows\n", day, len(res.Rows))
	}
	return sum, nil
}

// preliminary reports whether a day's data can still change: it is after the
// latest final date, or the response marks it (or one of its hours) incomplete.
func preliminary(d analytics.Date, dataState string, res *analytics.Result, finalThrough analytics.Date) bool {
	if !finalThrough.IsZero() && d.After(finalThrough) {
		return true
	}
	if dataState == "" || dataState == "final" {
		return false
	}
	day := d.String()
	if res.FirstIncompleteDate != "" && day >= res.FirstIncompleteDate {
		return true
	}
	// Hour markers carry a Pacific offset (2026-10-02T13:00:00-07:00), so the
	// first ten characters are the Pacific calendar date.
	return len(res.FirstIncompleteHour) >= 10 && day >= res.FirstIncompleteHour[:10]
}

func retryDelay(err error, attempt int) (time.Duration, bool) {
	var api *client.APIError
	if !errors.As(err, &api) || !(api.Quota() || api.Status >= 500) {
		return 0, false
	}
	if s, err := strconv.Atoi(api.RetryAfter); err == nil && s > 0 {
		return time.Duration(s) * time.Second, true
	}
	return min(time.Duration(5<<attempt)*time.Second, 2*time.Minute), true
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// writeDay writes a day's rows to a temp file and renames it into place, so
// an interrupted day never leaves partial output.
func writeDay(path, format string, res *analytics.Result) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".export-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	w := bufio.NewWriter(f)
	if format == "csv" {
		err = output.WriteCSV(w, res.Table())
	} else {
		for _, row := range res.Rows {
			b := analytics.RowJSON(res.Request.Dimensions, row)
			if _, err = w.Write(append(b, '\n')); err != nil {
				break
			}
		}
	}
	if err == nil {
		err = w.Flush()
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func loadManifest(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s is not a gsc export manifest", path)
	}
	if m.Days == nil {
		m.Days = map[string]DayRecord{}
	}
	return &m, nil
}

func saveManifest(path string, m *Manifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

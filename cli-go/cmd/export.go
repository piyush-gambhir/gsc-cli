package cmd

import (
	"errors"
	"time"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/analytics"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/export"
	"github.com/spf13/cobra"
)

func (a *app) export() *cobra.Command {
	q := &queryFlags{noRaw: true, noLimit: true, noCompare: true, defaultLast: "28d"}
	var out, format string
	var retry int
	var interval time.Duration
	var restart bool
	c := &cobra.Command{Use: "export", Short: "Export Search Analytics day by day to CSV or NDJSON files (resumable)", Args: cobra.NoArgs,
		Long: "Splits the range into days, as Google's extraction guide recommends, fetches every exposed row for each\n" +
			"day, and writes one file per day through a temp file and rename. manifest.json records finished days;\n" +
			"rerun the same command to resume. Days after the latest final date, and days Google marks incomplete,\n" +
			"are recorded as preliminary and re-fetched on resume.\n" +
			"No automatic retries unless --retry is given.",
		Example: "  gsc export --last 16m -d query -d page --out ./gsc-export\n  gsc export --start 2026-01-01 --end 2026-03-31 -d page --format ndjson --out ./q1",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if out == "" {
				return withKind(kindUsage, errors.New("--out DIR is required"))
			}
			if retry < 0 {
				return withKind(kindUsage, errors.New("--retry must not be negative"))
			}
			sp, err := a.buildSpec(cmd, q)
			if err != nil {
				return withKind(kindUsage, err)
			}
			cl, _, err := a.connect(ctx)
			if err != nil {
				return err
			}
			site, err := a.resolveSite(ctx, cl, "")
			if err != nil {
				return err
			}
			if err := a.finalize(ctx, cl, site, sp, false); err != nil {
				return err
			}
			start, _ := analytics.ParseDate(sp.req.StartDate)
			end, _ := analytics.ParseDate(sp.req.EndDate)
			// One small probe: days after the latest final date stay pending, so an
			// empty day whose data has not arrived is re-fetched on resume.
			finalThrough, _, err := a.latestDate(ctx, cl, site, sp.req, "final")
			if err != nil {
				return err
			}
			if finalThrough.IsZero() {
				finalThrough = a.today().AddDays(-11) // no final data in the probe window
			}
			sum, err := export.Run(ctx, cl, export.Options{Site: site, Base: sp.req, Range: analytics.Range{Start: start, End: end},
				Format: format, OutDir: out, Retry: retry, MinInterval: interval, Restart: restart, FinalThrough: finalThrough,
				Log: a.progressLog(), Sleep: a.sleep})
			if sum != nil {
				if perr := a.print(sum); perr != nil && err == nil {
					err = perr
				}
			}
			return err
		}}
	fl := c.Flags()
	q.register(fl)
	fl.StringVar(&out, "out", "", "Output directory (created if missing)")
	fl.StringVar(&format, "format", "csv", "File format: csv or ndjson")
	fl.IntVar(&retry, "retry", 0, "Retry quota and server errors this many times per day, with backoff (default 0)")
	fl.DurationVar(&interval, "interval", 250*time.Millisecond, "Minimum time between the start of one day and the next (a day's pages are fetched back to back)")
	fl.BoolVar(&restart, "restart", false, "Discard a manifest written for a different request")
	return c
}

func (a *app) progressLog() *quietWriter { return &quietWriter{a: a} }

// quietWriter sends progress lines to stderr unless --quiet.
type quietWriter struct{ a *app }

func (w *quietWriter) Write(p []byte) (int, error) {
	if w.a.quiet {
		return len(p), nil
	}
	return w.a.errOut.Write(p)
}

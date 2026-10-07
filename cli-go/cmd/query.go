package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/analytics"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type queryFlags struct {
	start, end, last                  string
	dims                              []string
	typ, dataState, aggregation       string
	filters                           []string
	limit                             int
	all                               bool
	compare                           string
	requestFile                       string
	printRequest, raw                 bool
	defaultLast                       string
	fixedDims                         []string // set by helper commands; nil means use --dimension
	noDims, noCompare, noRaw, noLimit bool
}

func (q *queryFlags) register(fs *pflag.FlagSet) {
	fs.StringVar(&q.start, "start", "", "Start date YYYY-MM-DD (Pacific Time, inclusive)")
	fs.StringVar(&q.end, "end", "", "End date YYYY-MM-DD (Pacific Time, inclusive)")
	fs.StringVar(&q.last, "last", "", "Window ending at the latest available date: 7d, 28d, 4w, 3m, 16m (default "+orDefault(q.defaultLast, "28d")+")")
	if q.fixedDims == nil && !q.noDims {
		fs.StringSliceVarP(&q.dims, "dimension", "d", nil, "Group by: date, hour, query, page, country, device, searchAppearance (repeat or comma list)")
	}
	fs.StringVar(&q.typ, "type", "", "Search type: web (default), image, video, news, discover, googleNews")
	fs.StringArrayVar(&q.filters, "filter", nil, "Filter 'DIM OP VALUE'; OP is = != ~ !~ =~ !=~ (contains/regex); repeat to AND")
	fs.StringVar(&q.dataState, "data-state", "", "final (default), all (includes preliminary data), or hourly_all")
	fs.StringVar(&q.aggregation, "aggregation", "", "auto, byPage, byProperty, or byNewsShowcasePanel")
	if !q.noLimit {
		fs.IntVar(&q.limit, "limit", q.limit, "Maximum rows to return (pages of up to 25,000 are fetched)")
		fs.BoolVar(&q.all, "all", false, "Fetch every row Google exposes for the query")
	}
	if !q.noCompare {
		fs.StringVar(&q.compare, "compare", "", "Also query a comparison period: previous or yoy")
	}
	if !q.noRaw {
		fs.StringVar(&q.requestFile, "request-file", "", "JSON request body to start from (flags override its fields; - for stdin)")
		fs.BoolVar(&q.printRequest, "print-request", false, "Print the request body and send nothing")
		fs.BoolVar(&q.raw, "raw", false, "Print the API response unchanged (single page, no --all or --compare)")
	}
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// spec is a request whose dates may still depend on the latest available day.
type spec struct {
	req  client.QueryRequest
	last string // pending --last window; empty when dates are explicit
	end  string // explicit --end that anchors --last
}

func (a *app) buildSpec(cmd *cobra.Command, q *queryFlags) (*spec, error) {
	sp := &spec{}
	if q.requestFile != "" {
		var b []byte
		var err error
		if q.requestFile == "-" {
			b, err = readAllLimited(a.reader())
		} else {
			b, err = os.ReadFile(q.requestFile)
		}
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, &sp.req); err != nil {
			return nil, fmt.Errorf("--request-file is not a valid searchAnalytics.query JSON body: %v", err)
		}
	}
	switch {
	case q.fixedDims != nil:
		sp.req.Dimensions = slices.Clone(q.fixedDims)
	case cmd.Flags().Changed("dimension"):
		sp.req.Dimensions = nil
		for _, d := range q.dims {
			if d = strings.TrimSpace(d); d != "" {
				sp.req.Dimensions = append(sp.req.Dimensions, d)
			}
		}
	}
	if q.typ != "" {
		sp.req.Type = q.typ
	}
	if q.dataState != "" {
		sp.req.DataState = q.dataState
	}
	if q.aggregation != "" {
		sp.req.AggregationType = q.aggregation
	}
	if len(q.filters) > 0 {
		group := client.FilterGroup{GroupType: "and"}
		for _, f := range q.filters {
			parsed, err := analytics.ParseFilter(f)
			if err != nil {
				return nil, err
			}
			group.Filters = append(group.Filters, parsed)
		}
		sp.req.DimensionFilterGroups = append(sp.req.DimensionFilterGroups, group)
	}
	switch {
	case q.start != "" && q.last != "":
		return nil, errors.New("use either --start/--end or --last, not both")
	case q.start != "":
		sp.req.StartDate, sp.req.EndDate = q.start, q.end
		if q.end == "" {
			sp.req.EndDate = a.today().String()
		}
	case q.last != "":
		sp.last, sp.end = q.last, q.end
	case q.end != "":
		return nil, errors.New("--end needs --start or --last")
	case sp.req.StartDate == "":
		sp.last = orDefault(q.defaultLast, "28d")
	}
	if q.limit < 0 {
		return nil, errors.New("--limit must be positive")
	}
	// A request file's rowLimit stands unless --limit was given explicitly.
	if f := cmd.Flags().Lookup("limit"); f != nil && !f.Changed && sp.req.RowLimit > 0 {
		q.limit = sp.req.RowLimit
	}
	if q.compare != "" && q.compare != "previous" && q.compare != "yoy" {
		return nil, fmt.Errorf("invalid --compare %q; use previous or yoy", q.compare)
	}
	if q.raw && (q.all || q.compare != "") {
		return nil, errors.New("--raw prints one API response; it cannot be combined with --all or --compare")
	}
	// Validate everything except the pending dates before any network call.
	probe := sp.req
	if sp.last != "" {
		end := a.today().AddDays(-1)
		if sp.end != "" {
			d, err := analytics.ParseDate(sp.end)
			if err != nil {
				return nil, err
			}
			end = d
		}
		r, err := analytics.LastRange(sp.last, end)
		if err != nil {
			return nil, err
		}
		probe.StartDate, probe.EndDate = r.Start.String(), r.End.String()
	}
	if _, err := analytics.Normalize(&probe, a.today()); err != nil {
		return nil, err
	}
	return sp, nil
}

func readAllLimited(r interface{ Read([]byte) (int, error) }) ([]byte, error) {
	var b bytes.Buffer
	_, err := b.ReadFrom(&limitedReader{r: r, n: 1 << 20})
	return b.Bytes(), err
}

type limitedReader struct {
	r interface{ Read([]byte) (int, error) }
	n int
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, errors.New("input exceeds 1 MiB")
	}
	if len(p) > l.n {
		p = p[:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= n
	return n, err
}

// latestDate finds the newest day with data for the data state (one small
// request). It is the anchor for --last.
func (a *app) latestDate(ctx context.Context, cl *client.Client, site string, req client.QueryRequest, state string) (analytics.Date, *client.QueryResponse, error) {
	today := a.today()
	probe := client.QueryRequest{StartDate: today.AddDays(-10).String(), EndDate: today.String(), Dimensions: []string{"date"},
		Type: req.Type, DataState: state, RowLimit: 25}
	resp, err := cl.Query(ctx, site, probe)
	if err != nil {
		return analytics.Date{}, nil, err
	}
	var latest analytics.Date
	for _, r := range resp.Rows {
		if len(r.Keys) == 0 {
			continue
		}
		if d, err := analytics.ParseDate(r.Keys[0]); err == nil && d.After(latest) {
			latest = d
		}
	}
	return latest, resp, nil
}

// finalize resolves pending --last dates, clips helper ranges to data
// retention, and validates the final request.
func (a *app) finalize(ctx context.Context, cl *client.Client, site string, sp *spec, estimateOnly bool) error {
	req := &sp.req
	if sp.last != "" {
		var end analytics.Date
		switch {
		case sp.end != "":
			d, err := analytics.ParseDate(sp.end)
			if err != nil {
				return withKind(kindUsage, err)
			}
			end = d
		case estimateOnly:
			end = a.today().AddDays(-1)
			a.info("Note: --print-request estimates the end date as %s; a real run uses the latest day with data.", end)
		default:
			state := "final"
			if req.DataState == "all" || req.DataState == "hourly_all" {
				state = "all"
			}
			latest, _, err := a.latestDate(ctx, cl, site, *req, state)
			if err != nil {
				return err
			}
			if latest.IsZero() {
				latest = a.today().AddDays(-3)
				a.warn("no %s data in the last 10 days; ending --last at %s", state, latest)
			} else {
				a.info("Latest %s data: %s", state, latest)
			}
			end = latest
		}
		r, err := analytics.LastRange(sp.last, end)
		if err != nil {
			return withKind(kindUsage, err)
		}
		oldest := analytics.RetentionStart(a.today())
		if slices.Contains(req.Dimensions, "hour") {
			oldest = analytics.HourlyStart(a.today())
		}
		if clipped, changed := analytics.Clip(r, oldest); changed {
			a.warn("--last %s starts before available data; clipped to %s..%s", sp.last, clipped.Start, clipped.End)
			r = clipped
		}
		req.StartDate, req.EndDate = r.Start.String(), r.End.String()
	}
	// Normalize validates the request offline: its errors are bad input.
	warnings, err := analytics.Normalize(req, a.today())
	if err != nil {
		return withKind(kindUsage, err)
	}
	for _, w := range warnings {
		a.warn("%s", w)
	}
	return nil
}

// runQuery executes a query spec and returns a printable result.
func (a *app) runQuery(cmd *cobra.Command, q *queryFlags) (any, *analytics.Result, error) {
	ctx := cmd.Context()
	sp, err := a.buildSpec(cmd, q)
	if err != nil {
		return nil, nil, withKind(kindUsage, err)
	}
	if q.printRequest {
		if err := a.finalize(ctx, nil, "", sp, true); err != nil {
			return nil, nil, err
		}
		if q.limit > 0 {
			sp.req.RowLimit = min(q.limit, analytics.MaxRowLimit)
		}
		return sp.req, nil, nil
	}
	cl, _, err := a.connect(ctx)
	if err != nil {
		return nil, nil, err
	}
	site, err := a.resolveSite(ctx, cl, "")
	if err != nil {
		return nil, nil, err
	}
	if err := a.finalize(ctx, cl, site, sp, false); err != nil {
		return nil, nil, err
	}
	if q.raw {
		req := sp.req
		req.RowLimit = min(max(q.limit, 1), analytics.MaxRowLimit)
		b, err := cl.QueryRaw(ctx, site, req)
		if err != nil {
			return nil, nil, err
		}
		return json.RawMessage(b), nil, nil
	}
	progress := func(n int) { a.info("Fetched %d rows...", n) }
	res, err := analytics.Fetch(ctx, cl, site, sp.req, q.limit, q.all, progress)
	if err != nil {
		return nil, nil, err
	}
	if q.compare == "" {
		return res, res, nil
	}
	start, _ := analytics.ParseDate(sp.req.StartDate)
	end, _ := analytics.ParseDate(sp.req.EndDate)
	prevRange, err := analytics.ComparePeriod(analytics.Range{Start: start, End: end}, q.compare)
	if err != nil {
		return nil, nil, withKind(kindUsage, err)
	}
	if clipped, changed := analytics.Clip(prevRange, analytics.RetentionStart(a.today())); changed {
		a.warn("comparison period starts before available data; clipped to %s..%s, so the periods differ in length", clipped.Start, clipped.End)
		prevRange = clipped
	}
	if prevRange.End.Before(prevRange.Start) {
		return nil, nil, errors.New("the comparison period is entirely outside Search Console's 16-month retention")
	}
	prevReq := sp.req
	prevReq.StartDate, prevReq.EndDate = prevRange.Start.String(), prevRange.End.String()
	prev, err := analytics.Fetch(ctx, cl, site, prevReq, q.limit, q.all, progress)
	if err != nil {
		return nil, nil, fmt.Errorf("comparison period: %w", err)
	}
	return analytics.Compare(q.compare, res, prev), res, nil
}

func (a *app) query() *cobra.Command {
	q := &queryFlags{limit: analytics.DefaultLimit}
	c := &cobra.Command{Use: "query", Short: "Search Analytics with every API option (dimensions, filters, types, data state)", Args: cobra.NoArgs,
		Long: "Runs searchAnalytics.query. Without dates it covers the last 28 days ending at the latest day with data\n" +
			"(one small extra request finds that day; pass --end to skip it). Results are sorted by clicks by Google.\n" +
			"\"complete\" in the output means every row Google exposes was fetched; anonymized queries are never included.",
		Example: "  gsc query -s sc-domain:example.com -d query -d page --limit 5000 -o csv\n" +
			"  gsc query --last 3m -d date --filter 'country = ind' --filter 'query ~ shoes'\n" +
			"  gsc query -d hour --data-state hourly_all --last 2d -o json\n" +
			"  gsc query -d query --compare previous --limit 500",
		RunE: func(cmd *cobra.Command, args []string) error {
			out, _, err := a.runQuery(cmd, q)
			if err != nil {
				return err
			}
			if raw, ok := out.(json.RawMessage); ok {
				return a.printRaw(raw)
			}
			return a.print(out)
		}}
	q.register(c.Flags())
	return c
}

// printRaw writes an API response unchanged (indented for JSON-friendly reading).
func (a *app) printRaw(b []byte) error {
	var buf bytes.Buffer
	if json.Indent(&buf, b, "", "  ") != nil {
		buf.Reset()
		buf.Write(b)
	}
	buf.WriteByte('\n')
	_, err := a.out.Write(buf.Bytes())
	return err
}

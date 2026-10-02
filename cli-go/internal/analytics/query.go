package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/output"
)

// Querier runs one Search Analytics request.
type Querier interface {
	Query(ctx context.Context, site string, req client.QueryRequest) (*client.QueryResponse, error)
}

const (
	DefaultLimit = 1000
	maxPages     = 2000
)

// Result is the envelope printed for Search Analytics commands.
type Result struct {
	Site                string
	Request             client.QueryRequest
	Aggregation         string
	FirstIncompleteDate string
	FirstIncompleteHour string
	MetadataReturned    bool
	// Complete means the last page was shorter than requested, so every row
	// Google exposes for this request was fetched. It says nothing about
	// anonymized queries or data finality.
	Complete bool
	Requests int
	// Rollup describes a local week or month rollup, if one was applied.
	Rollup string
	Rows   []client.Row
}

func (r *Result) header() []kv {
	return []kv{
		{"site", r.Site}, {"start", r.Request.StartDate}, {"end", r.Request.EndDate},
		{"type", orDefault(r.Request.Type, "web")}, {"data_state", orDefault(r.Request.DataState, "final")},
		{"dimensions", nonNil(r.Request.Dimensions)}, {"aggregation", nullable(r.Aggregation)},
		{"first_incomplete_date", nullable(r.FirstIncompleteDate)}, {"first_incomplete_hour", nullable(r.FirstIncompleteHour)},
		{"metadata_returned", r.MetadataReturned}, {"row_count", len(r.Rows)}, {"complete", r.Complete},
		{"requests", r.Requests}, {"rollup", nullable(r.Rollup)},
	}
}

func (r *Result) MarshalJSON() ([]byte, error) {
	rows := make([]json.RawMessage, len(r.Rows))
	for i, row := range r.Rows {
		rows[i] = rowObject(r.Request.Dimensions, row)
	}
	return object(append(r.header(), kv{"rows", rows})), nil
}

func (r *Result) Table() output.Table {
	t := output.Table{Columns: append(slices.Clone(r.Request.Dimensions), "clicks", "impressions", "ctr", "position"), Human: MetricFormats}
	for _, row := range r.Rows {
		t.Rows = append(t.Rows, rowCells(r.Request.Dimensions, row))
	}
	return t
}

// MetricFormats render metrics for terminal tables.
var MetricFormats = map[string]func(any) string{
	"ctr": output.Percent, "ctr_prev": output.Percent, "position": output.Fixed2, "position_prev": output.Fixed2,
	"position_delta": output.Fixed2, "clicks_pct": output.Percent, "impressions_pct": output.Percent,
	"ctr_delta_pp": output.Points, "expected_ctr": output.Percent, "share": output.Percent,
}

func rowCells(dims []string, row client.Row) []any {
	cells := make([]any, 0, len(dims)+4)
	for i := range dims {
		cells = append(cells, key(row, i))
	}
	return append(cells, row.Clicks, row.Impressions, row.CTR, row.Position)
}

func key(row client.Row, i int) any {
	if i < len(row.Keys) {
		return row.Keys[i]
	}
	return nil
}

func rowObject(dims []string, row client.Row) json.RawMessage {
	fields := make([]kv, 0, len(dims)+4)
	for i, d := range dims {
		fields = append(fields, kv{d, key(row, i)})
	}
	return object(append(fields, kv{"clicks", row.Clicks}, kv{"impressions", row.Impressions}, kv{"ctr", row.CTR}, kv{"position", row.Position}))
}

// Fetch pages through a query. limit bounds rows unless all is set; pages
// are at most 25,000 rows, advanced with startRow.
func Fetch(ctx context.Context, q Querier, site string, req client.QueryRequest, limit int, all bool, progress func(rows int)) (*Result, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	res := &Result{Site: site, Request: req, Rows: []client.Row{}}
	offset := req.StartRow
	var lastFirst []string
	for page := 0; ; page++ {
		if page >= maxPages {
			return nil, fmt.Errorf("stopped after %d pages without reaching the end; narrow the query", maxPages)
		}
		size := MaxRowLimit
		if !all {
			size = min(MaxRowLimit, limit-len(res.Rows))
		}
		pageReq := req
		pageReq.RowLimit, pageReq.StartRow = size, offset
		resp, err := q.Query(ctx, site, pageReq)
		res.Requests++
		if err != nil {
			if len(res.Rows) > 0 {
				return nil, fmt.Errorf("page %d failed after %d rows were fetched; nothing was printed: %w", page+1, len(res.Rows), err)
			}
			return nil, err
		}
		if page == 0 {
			res.Aggregation = resp.ResponseAggregationType
			if resp.Metadata != nil {
				res.MetadataReturned = true
				res.FirstIncompleteDate, res.FirstIncompleteHour = resp.Metadata.FirstIncompleteDate, resp.Metadata.FirstIncompleteHour
			}
		}
		if len(resp.Rows) > 0 {
			if page > 0 && slices.Equal(resp.Rows[0].Keys, lastFirst) && len(lastFirst) > 0 {
				return nil, fmt.Errorf("pagination did not advance at row %d; stopping to avoid duplicates", offset)
			}
			lastFirst = resp.Rows[0].Keys
		}
		// Google's extraction guide pages until an empty response; a short page
		// does not prove that no exposed rows remain.
		if len(resp.Rows) == 0 {
			res.Complete = true
			return res, nil
		}
		res.Rows = append(res.Rows, resp.Rows...)
		if progress != nil && page > 0 {
			progress(len(res.Rows))
		}
		offset += len(resp.Rows)
		if !all && len(res.Rows) >= limit {
			return res, nil
		}
	}
}

type kv struct {
	k string
	v any
}

// object encodes fields as a JSON object in the given order.
func object(fields []kv) json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range fields {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(f.k)
		v, err := json.Marshal(f.v)
		if err != nil {
			v = []byte("null")
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes()
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// RowJSON encodes one row as an ordered JSON object (dimensions, then metrics).
func RowJSON(dims []string, row client.Row) []byte { return rowObject(dims, row) }

package analytics

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/output"
)

// Comparison joins two periods on their dimension keys. A key missing from
// one period is unobserved, not zero: truncated or privacy-filtered results
// can omit rows that exist.
type Comparison struct {
	Mode     string
	Current  *Result
	Previous *Result
	Rows     []Pair
}

type Pair struct {
	Keys      []string
	Cur, Prev *client.Row
}

func Compare(mode string, cur, prev *Result) *Comparison {
	c := &Comparison{Mode: mode, Current: cur, Previous: prev}
	index := map[string]int{}
	for i := range cur.Rows {
		r := &cur.Rows[i]
		index[joinKeys(r.Keys)] = len(c.Rows)
		c.Rows = append(c.Rows, Pair{Keys: r.Keys, Cur: r})
	}
	for i := range prev.Rows {
		r := &prev.Rows[i]
		if at, ok := index[joinKeys(r.Keys)]; ok {
			c.Rows[at].Prev = r
			continue
		}
		c.Rows = append(c.Rows, Pair{Keys: r.Keys, Prev: r})
	}
	return c
}

func joinKeys(k []string) string { return strings.Join(k, "\x1f") }

// CompareColumns are the metric columns after the dimension columns.
var CompareColumns = []string{
	"clicks", "clicks_prev", "clicks_delta", "clicks_pct",
	"impressions", "impressions_prev", "impressions_delta", "impressions_pct",
	"ctr", "ctr_prev", "ctr_delta_pp",
	"position", "position_prev", "position_delta",
}

// Values returns the metric values for CompareColumns; nil means unknown.
func (p Pair) Values() []any {
	var cur, prev [4]*float64
	if p.Cur != nil {
		cur = [4]*float64{&p.Cur.Clicks, &p.Cur.Impressions, &p.Cur.CTR, &p.Cur.Position}
	}
	if p.Prev != nil {
		prev = [4]*float64{&p.Prev.Clicks, &p.Prev.Impressions, &p.Prev.CTR, &p.Prev.Position}
	}
	v := func(f *float64) any {
		if f == nil {
			return nil
		}
		return *f
	}
	both := p.Cur != nil && p.Prev != nil
	diff := func(i int, scale float64) any {
		if !both {
			return nil
		}
		return (*cur[i] - *prev[i]) * scale
	}
	pct := func(i int) any {
		if !both || *prev[i] == 0 {
			return nil
		}
		return (*cur[i] - *prev[i]) / *prev[i]
	}
	return []any{
		v(cur[0]), v(prev[0]), diff(0, 1), pct(0),
		v(cur[1]), v(prev[1]), diff(1, 1), pct(1),
		v(cur[2]), v(prev[2]), diff(2, 100),
		v(cur[3]), v(prev[3]), diff(3, 1),
	}
}

func (c *Comparison) MarshalJSON() ([]byte, error) {
	period := func(r *Result) json.RawMessage { return object(r.header()) }
	dims := c.Current.Request.Dimensions
	rows := make([]json.RawMessage, len(c.Rows))
	for i, p := range c.Rows {
		fields := make([]kv, 0, len(dims)+len(CompareColumns))
		for j, d := range dims {
			fields = append(fields, kv{d, p.Keys[j]})
		}
		for j, val := range p.Values() {
			fields = append(fields, kv{CompareColumns[j], val})
		}
		rows[i] = object(fields)
	}
	return object([]kv{
		{"mode", c.Mode}, {"current", period(c.Current)}, {"previous", period(c.Previous)},
		{"dimensions", nonNil(dims)},
		{"note", "null means the row was not returned for that period (not zero); ctr_delta_pp is in percentage points"},
		{"rows", rows},
	}), nil
}

func (c *Comparison) Table() output.Table {
	dims := c.Current.Request.Dimensions
	// Terminal tables show current values and changes; CSV, JSON, and YAML keep the previous values too.
	t := output.Table{Columns: append(slices.Clone(dims), CompareColumns...), Human: MetricFormats,
		HumanColumns: append(slices.Clone(dims), "clicks", "clicks_delta", "clicks_pct", "impressions", "impressions_delta",
			"ctr", "ctr_delta_pp", "position", "position_delta")}
	for _, p := range c.Rows {
		cells := make([]any, 0, len(t.Columns))
		for j := range dims {
			cells = append(cells, p.Keys[j])
		}
		t.Rows = append(t.Rows, append(cells, p.Values()...))
	}
	return t
}

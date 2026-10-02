package analytics

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/output"
)

// Insight is a locally computed view over explicit queries. Thresholds and
// the note travel with the rows so readers know exactly what was computed.
type Insight struct {
	Name       string
	Note       string
	Thresholds []kv
	Columns    []string
	Rows       [][]any
	Sources    []*Result
}

func (in *Insight) MarshalJSON() ([]byte, error) {
	rows := make([]json.RawMessage, len(in.Rows))
	for i, r := range in.Rows {
		fields := make([]kv, len(in.Columns))
		for j, c := range in.Columns {
			fields[j] = kv{c, r[j]}
		}
		rows[i] = object(fields)
	}
	sources := make([]json.RawMessage, len(in.Sources))
	for i, s := range in.Sources {
		sources[i] = object(s.header())
	}
	return object([]kv{{"insight", in.Name}, {"note", in.Note}, {"thresholds", object(in.Thresholds)}, {"sources", sources}, {"rows", rows}}), nil
}

func (in *Insight) Table() output.Table {
	return output.Table{Columns: in.Columns, Rows: in.Rows, Human: MetricFormats}
}

// Describe summarizes thresholds for stderr.
func (in *Insight) Describe() string {
	s := in.Name + ":"
	for _, t := range in.Thresholds {
		s += fmt.Sprintf(" %s=%v", t.k, t.v)
	}
	return s
}

func limitRows(rows [][]any, limit int) [][]any {
	if limit > 0 && len(rows) > limit {
		return rows[:limit]
	}
	return rows
}

// StrikingDistance lists queries ranking just off the top positions with
// enough impressions to matter.
func StrikingDistance(src *Result, minPos, maxPos, minImpressions float64, limit int) *Insight {
	in := &Insight{Name: "striking-distance", Sources: []*Result{src},
		Note:       "Queries whose average position is in the range, sorted by impressions. Average position is per query across all pages.",
		Thresholds: []kv{{"min_position", minPos}, {"max_position", maxPos}, {"min_impressions", minImpressions}},
		Columns:    []string{"query", "clicks", "impressions", "ctr", "position"}}
	rows := slices.Clone(src.Rows)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Impressions > rows[j].Impressions })
	for _, r := range rows {
		if r.Position >= minPos && r.Position <= maxPos && r.Impressions >= minImpressions {
			in.Rows = append(in.Rows, []any{key(r, 0), r.Clicks, r.Impressions, r.CTR, r.Position})
		}
	}
	in.Rows = limitRows(in.Rows, limit)
	return in
}

func positionBucket(p float64) string {
	switch {
	case p <= 10:
		return fmt.Sprint(int(math.Max(1, math.Round(p))))
	case p <= 20:
		return "11-20"
	default:
		return "21+"
	}
}

// LowCTR flags queries whose CTR is well below the site's own median CTR for
// the same position bucket. Buckets with fewer than three qualifying queries
// are skipped because a median of one or two rows is not meaningful.
func LowCTR(src *Result, minImpressions, factor float64, limit int) *Insight {
	in := &Insight{Name: "low-ctr", Sources: []*Result{src},
		Note:       "expected_ctr is this site's median CTR among queries in the same position bucket; missed_clicks_estimate = impressions x (expected_ctr - ctr). Estimates, not forecasts.",
		Thresholds: []kv{{"min_impressions", minImpressions}, {"ctr_below_fraction_of_median", factor}, {"min_queries_per_bucket", 3}},
		Columns:    []string{"query", "clicks", "impressions", "ctr", "position", "position_bucket", "expected_ctr", "missed_clicks_estimate"}}
	buckets := map[string][]float64{}
	var eligible []client.Row
	for _, r := range src.Rows {
		if r.Impressions >= minImpressions {
			eligible = append(eligible, r)
			b := positionBucket(r.Position)
			buckets[b] = append(buckets[b], r.CTR)
		}
	}
	median := map[string]float64{}
	for b, v := range buckets {
		if len(v) >= 3 {
			sort.Float64s(v)
			m := v[len(v)/2]
			if len(v)%2 == 0 {
				m = (v[len(v)/2-1] + v[len(v)/2]) / 2
			}
			median[b] = m
		}
	}
	type flagged struct {
		r        client.Row
		b        string
		expected float64
		missed   float64
	}
	var hits []flagged
	for _, r := range eligible {
		b := positionBucket(r.Position)
		m, ok := median[b]
		if ok && r.CTR < factor*m {
			hits = append(hits, flagged{r, b, m, r.Impressions * (m - r.CTR)})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].missed > hits[j].missed })
	for _, h := range hits {
		in.Rows = append(in.Rows, []any{key(h.r, 0), h.r.Clicks, h.r.Impressions, h.r.CTR, h.r.Position, h.b, h.expected, math.Round(h.missed*10) / 10})
	}
	in.Rows = limitRows(in.Rows, limit)
	return in
}

// Cannibalization finds queries where two or more pages each take a
// meaningful share of the query's impressions. src must be grouped by query
// then page.
func Cannibalization(src *Result, minShare, minQueryImpressions float64, limit int) *Insight {
	in := &Insight{Name: "cannibalization", Sources: []*Result{src},
		Note:       "share is the page's fraction of the query's impressions among returned rows. Rows are one per competing page, grouped by query.",
		Thresholds: []kv{{"min_page_share", minShare}, {"min_query_impressions", minQueryImpressions}, {"min_pages", 2}},
		Columns:    []string{"query", "page", "clicks", "impressions", "share", "position"}}
	type group struct {
		total float64
		rows  []client.Row
	}
	groups := map[string]*group{}
	var order []string
	for _, r := range src.Rows {
		q, _ := key(r, 0).(string)
		g, ok := groups[q]
		if !ok {
			g = &group{}
			groups[q] = g
			order = append(order, q)
		}
		g.total += r.Impressions
		g.rows = append(g.rows, r)
	}
	sort.SliceStable(order, func(i, j int) bool { return groups[order[i]].total > groups[order[j]].total })
	queries := 0
	for _, q := range order {
		g := groups[q]
		if g.total < minQueryImpressions {
			continue
		}
		var competing []client.Row
		for _, r := range g.rows {
			if r.Impressions/g.total >= minShare {
				competing = append(competing, r)
			}
		}
		if len(competing) < 2 {
			continue
		}
		if limit > 0 && queries >= limit {
			break
		}
		queries++
		sort.SliceStable(competing, func(i, j int) bool { return competing[i].Impressions > competing[j].Impressions })
		for _, r := range competing {
			in.Rows = append(in.Rows, []any{q, key(r, 1), r.Clicks, r.Impressions, r.Impressions / g.total, r.Position})
		}
	}
	return in
}

// Decliners lists rows observed in both periods whose clicks fell.
func Decliners(c *Comparison, minPrevClicks float64, limit int) *Insight {
	dims := c.Current.Request.Dimensions
	in := &Insight{Name: "decliners", Sources: []*Result{c.Current, c.Previous},
		Note:       "Only rows returned in both periods; rows missing from one period are excluded rather than treated as zero.",
		Thresholds: []kv{{"compare", c.Mode}, {"min_previous_clicks", minPrevClicks}},
		Columns:    append(slices.Clone(dims), "clicks", "clicks_prev", "clicks_delta", "clicks_pct", "impressions", "impressions_prev", "position", "position_prev")}
	var pairs []Pair
	for _, p := range c.Rows {
		if p.Cur != nil && p.Prev != nil && p.Prev.Clicks >= minPrevClicks && p.Cur.Clicks < p.Prev.Clicks {
			pairs = append(pairs, p)
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		return pairs[i].Cur.Clicks-pairs[i].Prev.Clicks < pairs[j].Cur.Clicks-pairs[j].Prev.Clicks
	})
	for _, p := range pairs {
		v := p.Values()
		row := make([]any, 0, len(in.Columns))
		for i := range dims {
			row = append(row, p.Keys[i])
		}
		in.Rows = append(in.Rows, append(row, v[0], v[1], v[2], v[3], v[4], v[5], v[11], v[12]))
	}
	in.Rows = limitRows(in.Rows, limit)
	return in
}

// Exposure lists rows returned in only one period: "new" (current only) or
// "lost" (previous only). Absence means "not among the rows Google returned
// within the fetched limit", which is not proof that the query disappeared.
func Exposure(c *Comparison, which string, limit int) *Insight {
	dims := c.Current.Request.Dimensions
	name, note := "new-queries", "Rows returned for the current period but not for the comparison period (newly exposed within the fetched limit)."
	if which == "lost" {
		name, note = "lost-queries", "Rows returned for the comparison period but not for the current period (no longer exposed within the fetched limit)."
	}
	in := &Insight{Name: name, Note: note, Sources: []*Result{c.Current, c.Previous},
		Thresholds: []kv{{"compare", c.Mode}},
		Columns:    append(slices.Clone(dims), "clicks", "impressions", "ctr", "position")}
	var rows []client.Row
	for _, p := range c.Rows {
		if which == "new" && p.Cur != nil && p.Prev == nil {
			rows = append(rows, *p.Cur)
		}
		if which == "lost" && p.Prev != nil && p.Cur == nil {
			rows = append(rows, *p.Prev)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Clicks != rows[j].Clicks {
			return rows[i].Clicks > rows[j].Clicks
		}
		return rows[i].Impressions > rows[j].Impressions
	})
	for _, r := range rows {
		in.Rows = append(in.Rows, rowCells(dims, r))
	}
	in.Rows = limitRows(in.Rows, limit)
	return in
}

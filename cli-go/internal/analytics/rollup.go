package analytics

import (
	"fmt"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
)

// RollupMethod is reported alongside rolled-up results.
const RollupMethod = "clicks and impressions summed; CTR recomputed from the sums; position weighted by impressions"

// Rollup groups date rows (single "date" dimension) into ISO weeks (labelled
// by their Monday) or calendar months. Rows must be disjoint days of one
// query, which is what a date-grouped query returns.
func Rollup(rows []client.Row, by string) ([]client.Row, error) {
	if by != "week" && by != "month" {
		return nil, fmt.Errorf("rollup must be week or month")
	}
	var out []client.Row
	index := map[string]int{}
	weighted := map[string]float64{}
	for _, r := range rows {
		if len(r.Keys) != 1 {
			return nil, fmt.Errorf("rollup needs rows grouped only by date")
		}
		d, err := ParseDate(r.Keys[0])
		if err != nil {
			return nil, err
		}
		label := d.Time().Format("2006-01")
		if by == "week" {
			offset := (int(d.Time().Weekday()) + 6) % 7 // days since Monday
			label = d.AddDays(-offset).String()
		}
		i, ok := index[label]
		if !ok {
			i = len(out)
			index[label] = i
			out = append(out, client.Row{Keys: []string{label}})
		}
		out[i].Clicks += r.Clicks
		out[i].Impressions += r.Impressions
		weighted[label] += r.Position * r.Impressions
	}
	for i := range out {
		label := out[i].Keys[0]
		if out[i].Impressions > 0 {
			out[i].CTR = out[i].Clicks / out[i].Impressions
			out[i].Position = weighted[label] / out[i].Impressions
		}
	}
	return out, nil
}

package analytics

import (
	"fmt"
	"strings"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
)

var (
	SearchTypes  = []string{"web", "image", "video", "news", "discover", "googleNews"}
	DataStates   = []string{"final", "all", "hourly_all"}
	Aggregations = []string{"auto", "byPage", "byProperty", "byNewsShowcasePanel"}
	operatorsAPI = []string{"equals", "notEquals", "contains", "notContains", "includingRegex", "excludingRegex"}
)

const MaxRowLimit = 25000

// Normalize canonicalizes spellings in place and validates the request
// against restrictions Google documents. It returns warnings for things that
// are not errors, such as dates outside data retention.
func Normalize(req *client.QueryRequest, today Date) ([]string, error) {
	var warnings []string
	start, err := ParseDate(req.StartDate)
	if err != nil {
		return nil, fmt.Errorf("start date: %w", err)
	}
	end, err := ParseDate(req.EndDate)
	if err != nil {
		return nil, fmt.Errorf("end date: %w", err)
	}
	if end.Before(start) {
		return nil, fmt.Errorf("start date %s is after end date %s", start, end)
	}
	seen := map[string]bool{}
	for i, d := range req.Dimensions {
		c, ok := CanonicalDimension(d, Dimensions)
		if !ok {
			return nil, fmt.Errorf("unknown dimension %q; use %s", d, strings.Join(Dimensions, ", "))
		}
		if seen[c] {
			return nil, fmt.Errorf("dimension %q is repeated", c)
		}
		seen[c] = true
		req.Dimensions[i] = c
	}
	if req.Type != "" {
		c, ok := CanonicalDimension(req.Type, SearchTypes)
		if !ok {
			return nil, fmt.Errorf("unknown --type %q; use %s", req.Type, strings.Join(SearchTypes, ", "))
		}
		req.Type = c
	}
	if req.DataState != "" {
		c, ok := CanonicalDimension(req.DataState, DataStates)
		if !ok {
			return nil, fmt.Errorf("unknown --data-state %q; use %s", req.DataState, strings.Join(DataStates, ", "))
		}
		req.DataState = c
	}
	if req.AggregationType != "" {
		c, ok := CanonicalDimension(req.AggregationType, Aggregations)
		if !ok {
			return nil, fmt.Errorf("unknown --aggregation %q; use %s", req.AggregationType, strings.Join(Aggregations, ", "))
		}
		req.AggregationType = c
	}
	if req.RowLimit < 0 || req.RowLimit > MaxRowLimit {
		return nil, fmt.Errorf("rowLimit must be between 1 and %d", MaxRowLimit)
	}
	if req.StartRow < 0 {
		return nil, fmt.Errorf("startRow must not be negative")
	}
	pageFiltered, appearanceFilters, showcase := false, 0, false
	for gi := range req.DimensionFilterGroups {
		g := &req.DimensionFilterGroups[gi]
		if g.GroupType != "" && !strings.EqualFold(g.GroupType, "and") {
			return nil, fmt.Errorf("filter groupType %q is not supported; Search Console only accepts \"and\"", g.GroupType)
		}
		for fi := range g.Filters {
			f := &g.Filters[fi]
			c, ok := CanonicalDimension(f.Dimension, filterDimensions)
			if !ok {
				return nil, fmt.Errorf("cannot filter on %q", f.Dimension)
			}
			f.Dimension = c
			if f.Operator != "" {
				op, ok := CanonicalDimension(f.Operator, operatorsAPI)
				if !ok {
					return nil, fmt.Errorf("unknown filter operator %q", f.Operator)
				}
				f.Operator = op
			}
			switch c {
			case "page":
				pageFiltered = true
			case "searchAppearance":
				appearanceFilters++
				if (f.Operator == "" || f.Operator == "equals") && strings.EqualFold(f.Expression, "NEWS_SHOWCASE") {
					showcase = true
				}
			}
		}
	}
	if seen["hour"] && req.DataState != "hourly_all" {
		return nil, fmt.Errorf("the hour dimension requires --data-state hourly_all")
	}
	pageGrouped := seen["page"]
	switch req.AggregationType {
	case "byProperty":
		if pageGrouped || pageFiltered {
			return nil, fmt.Errorf("--aggregation byProperty cannot be combined with page grouping or a page filter")
		}
		if req.Type == "discover" || req.Type == "googleNews" {
			return nil, fmt.Errorf("--aggregation byProperty is not supported for --type %s", req.Type)
		}
	case "byNewsShowcasePanel":
		if !showcase || appearanceFilters != 1 {
			return nil, fmt.Errorf("--aggregation byNewsShowcasePanel requires exactly one searchAppearance filter equal to NEWS_SHOWCASE")
		}
		if req.Type != "discover" && req.Type != "googleNews" {
			return nil, fmt.Errorf("--aggregation byNewsShowcasePanel requires --type discover or googleNews")
		}
		if pageGrouped || pageFiltered {
			return nil, fmt.Errorf("--aggregation byNewsShowcasePanel cannot be combined with page grouping or a page filter")
		}
	}
	if seen["searchAppearance"] && len(req.Dimensions) > 1 {
		warnings = append(warnings, "Google's extraction guide says searchAppearance cannot be grouped with other dimensions; list appearances first (gsc top appearance), then filter on one")
	}
	if start.Before(RetentionStart(today)) {
		warnings = append(warnings, fmt.Sprintf("Search Console keeps about 16 months of data; days before %s will be empty", RetentionStart(today)))
	}
	if seen["hour"] && start.Before(HourlyStart(today)) {
		warnings = append(warnings, fmt.Sprintf("hourly data covers about the last 10 days; hours before %s will be empty", HourlyStart(today)))
	}
	if end.After(today) {
		warnings = append(warnings, fmt.Sprintf("end date %s is in the future (Pacific Time)", end))
	}
	return warnings, nil
}

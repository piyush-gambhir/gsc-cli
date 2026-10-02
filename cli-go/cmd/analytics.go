package cmd

import (
	"fmt"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/analytics"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
	"github.com/spf13/cobra"
)

func (a *app) performance() *cobra.Command {
	q := &queryFlags{fixedDims: []string{}, noRaw: true, noLimit: true}
	c := &cobra.Command{Use: "performance", Short: "Totals for a period: clicks, impressions, CTR, average position", Args: cobra.NoArgs,
		Long:    "A query with no dimensions, so totals match the Performance report chart (anonymized queries included).",
		Example: "  gsc performance --last 28d --compare previous\n  gsc performance --type discover --last 3m -o json",
		RunE: func(cmd *cobra.Command, args []string) error {
			out, _, err := a.runQuery(cmd, q)
			if err != nil {
				return err
			}
			return a.print(out)
		}}
	q.register(c.Flags())
	return c
}

func (a *app) top() *cobra.Command {
	c := &cobra.Command{Use: "top", Short: "Top queries, pages, countries, devices, or search appearances"}
	for _, t := range []struct{ use, dim, short string }{
		{"queries", "query", "Top search queries by clicks"},
		{"pages", "page", "Top pages by clicks"},
		{"countries", "country", "Clicks and impressions by country (ISO 3166-1 alpha-3)"},
		{"devices", "device", "Clicks and impressions by device"},
		{"appearance", "searchAppearance", "Search appearance types (filter on one with --filter 'searchAppearance = VALUE' in gsc query)"},
	} {
		q := &queryFlags{fixedDims: []string{t.dim}, limit: 25, noRaw: true}
		sub := &cobra.Command{Use: t.use, Short: t.short, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			out, _, err := a.runQuery(cmd, q)
			if err != nil {
				return err
			}
			return a.print(out)
		}}
		q.register(sub.Flags())
		c.AddCommand(sub)
	}
	return c
}

func (a *app) trend() *cobra.Command {
	var by string
	q := &queryFlags{noDims: true, noRaw: true, noLimit: true, noCompare: true}
	c := &cobra.Command{Use: "trend", Short: "Clicks, impressions, CTR, and position over time", Args: cobra.NoArgs,
		Long: "Groups by date (default), hour (last 10 days, preliminary data), ISO week, or calendar month.\n" +
			"Week and month totals are rolled up locally from daily rows: " + analytics.RollupMethod + ".",
		Example: "  gsc trend --last 3m --by week\n  gsc trend --by hour --last 2d\n  gsc trend --last 16m --by month --filter 'page ~ /blog/'",
		RunE: func(cmd *cobra.Command, args []string) error {
			switch by {
			case "date", "week", "month":
				q.fixedDims = []string{"date"}
			case "hour":
				q.fixedDims = []string{"hour"}
				if q.dataState != "" && q.dataState != "hourly_all" {
					return fmt.Errorf("--by hour requires --data-state hourly_all")
				}
				q.dataState = "hourly_all"
				if q.last == "" && q.start == "" {
					q.last = "2d"
				}
			default:
				return fmt.Errorf("--by must be date, hour, week, or month")
			}
			q.all = true
			_, res, err := a.runQuery(cmd, q)
			if err != nil {
				return err
			}
			if by == "week" || by == "month" {
				rows, err := analytics.Rollup(res.Rows, by)
				if err != nil {
					return err
				}
				res.Rows = rows
				res.Request.Dimensions = []string{by}
				res.Rollup = analytics.RollupMethod
				a.info("Rolled up by %s: %s.", by, analytics.RollupMethod)
			}
			return a.print(res)
		}}
	c.Flags().StringVar(&by, "by", "date", "Granularity: date, hour, week, or month")
	q.register(c.Flags())
	return c
}

func (a *app) freshness() *cobra.Command {
	var typ string
	c := &cobra.Command{Use: "freshness", Short: "Latest dates with final and with preliminary data", Args: cobra.NoArgs,
		Long: "Two small date-grouped queries over the last 10 days: one for final data and one including preliminary\n" +
			"data. Final data usually lags two to three days.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, _, err := a.connect(ctx)
			if err != nil {
				return err
			}
			site, err := a.resolveSite(ctx, cl, "")
			if err != nil {
				return err
			}
			req := client.QueryRequest{Type: typ}
			final, _, err := a.latestDate(ctx, cl, site, req, "final")
			if err != nil {
				return err
			}
			prelim, resp, err := a.latestDate(ctx, cl, site, req, "all")
			if err != nil {
				return err
			}
			out := map[string]any{"site": site, "type": orDefault(typ, "web"), "checked_on": a.today().String(), "timezone": "America/Los_Angeles",
				"latest_final_date": dateOrNil(final), "latest_preliminary_date": dateOrNil(prelim), "first_incomplete_date": nil}
			if resp != nil && resp.Metadata != nil && resp.Metadata.FirstIncompleteDate != "" {
				out["first_incomplete_date"] = resp.Metadata.FirstIncompleteDate
			}
			return a.print(out)
		}}
	c.Flags().StringVar(&typ, "type", "", "Search type (default web)")
	return c
}

func dateOrNil(d analytics.Date) any {
	if d.IsZero() {
		return nil
	}
	return d.String()
}

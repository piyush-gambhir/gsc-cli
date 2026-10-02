package cmd

import (
	"fmt"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/analytics"
	"github.com/spf13/cobra"
)

// insightFetchLimit bounds rows fetched for local analysis.
const insightFetchLimit = 25000

func (a *app) insights() *cobra.Command {
	c := &cobra.Command{Use: "insights", Short: "Opportunities computed locally from explicit queries (thresholds are printed)",
		Long: "Each insight runs ordinary Search Analytics queries and computes the result locally. The thresholds used\n" +
			"are printed on stderr and included in JSON output. Results reflect rows Google exposes; anonymized queries\n" +
			"are never included."}
	c.AddCommand(a.insightStriking(), a.insightLowCTR(), a.insightCannibalization(), a.insightDecliners(),
		a.insightExposure("new-queries", "new"), a.insightExposure("lost-queries", "lost"))
	return c
}

func (a *app) emitInsight(in *analytics.Insight) error {
	a.info("%s", in.Describe())
	return a.print(in)
}

func (a *app) insightStriking() *cobra.Command {
	q := &queryFlags{fixedDims: []string{"query"}, noRaw: true, noLimit: true, noCompare: true}
	var minPos, maxPos, minImpr float64
	var limit int
	c := &cobra.Command{Use: "striking-distance", Short: "Queries ranking just off the top (average position 4 to 20) with real impressions", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if minPos > maxPos {
				return fmt.Errorf("--min-position must not exceed --max-position")
			}
			q.limit = insightFetchLimit
			_, res, err := a.runQuery(cmd, q)
			if err != nil {
				return err
			}
			return a.emitInsight(analytics.StrikingDistance(res, minPos, maxPos, minImpr, limit))
		}}
	q.register(c.Flags())
	c.Flags().Float64Var(&minPos, "min-position", 4, "Lowest average position to include")
	c.Flags().Float64Var(&maxPos, "max-position", 20, "Highest average position to include")
	c.Flags().Float64Var(&minImpr, "min-impressions", 100, "Minimum impressions in the period")
	c.Flags().IntVar(&limit, "limit", 50, "Maximum rows to show")
	return c
}

func (a *app) insightLowCTR() *cobra.Command {
	q := &queryFlags{fixedDims: []string{"query"}, noRaw: true, noLimit: true, noCompare: true}
	var minImpr, factor float64
	var limit int
	c := &cobra.Command{Use: "low-ctr", Short: "Queries whose CTR is well below this site's median for the same position", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if factor <= 0 || factor >= 1 {
				return fmt.Errorf("--factor must be between 0 and 1")
			}
			q.limit = insightFetchLimit
			_, res, err := a.runQuery(cmd, q)
			if err != nil {
				return err
			}
			return a.emitInsight(analytics.LowCTR(res, minImpr, factor, limit))
		}}
	q.register(c.Flags())
	c.Flags().Float64Var(&minImpr, "min-impressions", 100, "Minimum impressions for a query to count")
	c.Flags().Float64Var(&factor, "factor", 0.5, "Flag queries with CTR below this fraction of the bucket median")
	c.Flags().IntVar(&limit, "limit", 50, "Maximum rows to show")
	return c
}

func (a *app) insightCannibalization() *cobra.Command {
	q := &queryFlags{fixedDims: []string{"query", "page"}, noRaw: true, noLimit: true, noCompare: true}
	var minShare, minImpr float64
	var limit int
	c := &cobra.Command{Use: "cannibalization", Short: "Queries where two or more pages split the impressions", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if minShare <= 0 || minShare >= 1 {
				return fmt.Errorf("--min-share must be between 0 and 1")
			}
			q.limit = insightFetchLimit
			_, res, err := a.runQuery(cmd, q)
			if err != nil {
				return err
			}
			return a.emitInsight(analytics.Cannibalization(res, minShare, minImpr, limit))
		}}
	q.register(c.Flags())
	c.Flags().Float64Var(&minShare, "min-share", 0.1, "Minimum share of a query's impressions for a page to count as competing")
	c.Flags().Float64Var(&minImpr, "min-impressions", 100, "Minimum total impressions for a query")
	c.Flags().IntVar(&limit, "limit", 25, "Maximum queries to show")
	return c
}

func (a *app) comparisonInsight(use, short string, build func(*analytics.Comparison, int) *analytics.Insight, extra func(*cobra.Command)) *cobra.Command {
	q := &queryFlags{noRaw: true, noLimit: true, noCompare: true}
	var dim, compare string
	var limit int
	c := &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dim != "query" && dim != "page" {
				return fmt.Errorf("--by must be query or page")
			}
			q.fixedDims, q.compare, q.limit = []string{dim}, compare, insightFetchLimit
			out, _, err := a.runQuery(cmd, q)
			if err != nil {
				return err
			}
			comp, ok := out.(*analytics.Comparison)
			if !ok {
				return fmt.Errorf("comparison did not run")
			}
			return a.emitInsight(build(comp, limit))
		}}
	q.noDims = true
	q.register(c.Flags())
	c.Flags().StringVar(&dim, "by", "query", "Compare queries or pages")
	c.Flags().StringVar(&compare, "compare", "previous", "Comparison period: previous or yoy")
	c.Flags().IntVar(&limit, "limit", 50, "Maximum rows to show")
	if extra != nil {
		extra(c)
	}
	return c
}

func (a *app) insightDecliners() *cobra.Command {
	var minClicks float64
	return a.comparisonInsight("decliners", "Queries or pages that lost the most clicks versus the comparison period",
		func(c *analytics.Comparison, limit int) *analytics.Insight {
			return analytics.Decliners(c, minClicks, limit)
		},
		func(cmd *cobra.Command) {
			cmd.Flags().Float64Var(&minClicks, "min-clicks", 5, "Minimum clicks in the comparison period")
		})
}

func (a *app) insightExposure(use, which string) *cobra.Command {
	short := "Queries or pages returned now but not in the comparison period (newly exposed)"
	if which == "lost" {
		short = "Queries or pages returned in the comparison period but not now (no longer exposed)"
	}
	return a.comparisonInsight(use, short, func(c *analytics.Comparison, limit int) *analytics.Insight {
		return analytics.Exposure(c, which, limit)
	}, nil)
}

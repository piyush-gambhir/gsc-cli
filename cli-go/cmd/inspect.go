package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/output"
	"github.com/spf13/cobra"
)

type inspection struct {
	URL    string         `json:"url"`
	Result map[string]any `json:"result,omitempty"`
	Error  string         `json:"error,omitempty"`
}

type inspectReport struct {
	Site      string       `json:"site"`
	Requested int          `json:"requested"`
	Inspected int          `json:"inspected"`
	Stopped   string       `json:"stopped_reason,omitempty"`
	Note      string       `json:"note"`
	Results   []inspection `json:"results"`
}

var inspectColumns = []string{"url", "verdict", "coverage_state", "indexing_state", "last_crawl_time", "page_fetch_state", "robots_txt_state", "google_canonical", "user_canonical", "crawled_as", "rich_results", "error"}

func (r *inspectReport) Table() output.Table {
	t := output.Table{Columns: inspectColumns, Human: map[string]func(any) string{"last_crawl_time": output.Timestamp},
		HumanColumns: []string{"url", "verdict", "coverage_state", "last_crawl_time", "google_canonical", "error"}}
	for _, in := range r.Results {
		idx, _ := in.Result["indexStatusResult"].(map[string]any)
		rich, _ := in.Result["richResultsResult"].(map[string]any)
		get := func(m map[string]any, k string) any {
			if m == nil || m[k] == nil {
				return nil
			}
			return m[k]
		}
		var errCell any
		if in.Error != "" {
			errCell = in.Error
		}
		t.Rows = append(t.Rows, []any{in.URL, get(idx, "verdict"), get(idx, "coverageState"), get(idx, "indexingState"), get(idx, "lastCrawlTime"),
			get(idx, "pageFetchState"), get(idx, "robotsTxtState"), get(idx, "googleCanonical"), get(idx, "userCanonical"), get(idx, "crawledAs"),
			get(rich, "verdict"), errCell})
	}
	return t
}

func (a *app) inspect() *cobra.Command {
	var file, language string
	var maxURLs int
	var interval time.Duration
	c := &cobra.Command{Use: "inspect [URL...]", Short: "URL Inspection: Google's indexed view of URLs (not a live test)",
		Long: "Calls the URL Inspection API for each URL. It reports the indexed version only: there is no live test and\n" +
			"no request-indexing in the API. Google allows 2,000 inspections per property per day and 600 per minute;\n" +
			"batches are paced below that and stop cleanly when a quota is reached.",
		Example: "  gsc inspect https://www.example.com/pricing\n  gsc inspect --file urls.txt -o json\n  cat urls.txt | gsc inspect --file - --max 200",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			urls := append([]string{}, args...)
			if file != "" {
				more, err := a.readURLs(file)
				if err != nil {
					return err
				}
				urls = append(urls, more...)
			}
			urls = dedupe(urls)
			if len(urls) == 0 {
				return withKind(kindUsage, errors.New("give URLs as arguments or with --file"))
			}
			for _, u := range urls {
				if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
					return fmt.Errorf("%q is not an absolute http(s) URL", u)
				}
			}
			if maxURLs <= 0 {
				return errors.New("--max must be positive")
			}
			if len(urls) > maxURLs {
				a.warn("%d URLs given; inspecting the first %d (--max)", len(urls), maxURLs)
				urls = urls[:maxURLs]
			}
			cl, _, err := a.connect(ctx)
			if err != nil {
				return err
			}
			site, err := a.resolveSite(ctx, cl, "")
			if err != nil {
				return err
			}
			report := &inspectReport{Site: site, Requested: len(urls), Results: []inspection{},
				Note: "Indexed version as last crawled by Google; not a live test."}
			var stopErr error
			for i, u := range urls {
				if i > 0 && interval > 0 {
					if err := a.wait(ctx, interval); err != nil {
						stopErr = err
						break
					}
				}
				res, err := cl.Inspect(ctx, u, site, language)
				if err != nil {
					var api *client.APIError
					if errors.As(err, &api) && (api.Quota() || api.Status == 401) || ctx.Err() != nil {
						report.Stopped = err.Error()
						stopErr = err
						break
					}
					report.Results = append(report.Results, inspection{URL: u, Error: err.Error()})
					continue
				}
				report.Results = append(report.Results, inspection{URL: u, Result: res})
				report.Inspected++
				if len(urls) > 1 && (i+1)%25 == 0 {
					a.info("Inspected %d of %d...", i+1, len(urls))
				}
			}
			if err := a.print(report); err != nil {
				return err
			}
			if stopErr != nil {
				return fmt.Errorf("stopped after %d of %d URLs: %w", report.Inspected, len(urls), stopErr)
			}
			return nil
		}}
	c.Flags().StringVar(&file, "file", "", "Read URLs from a file, one per line (- for stdin)")
	c.Flags().StringVar(&language, "language", "", "BCP 47 language for issue messages (default en-US)")
	c.Flags().IntVar(&maxURLs, "max", 2000, "Inspect at most this many URLs (the per-property daily quota is 2,000)")
	c.Flags().DurationVar(&interval, "interval", 120*time.Millisecond, "Pause between inspections (stays under 600 per minute)")
	return c
}

func (a *app) readURLs(file string) ([]string, error) {
	var r io.Reader
	if file == "-" {
		r = a.reader()
	} else {
		f, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	var urls []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" && !strings.HasPrefix(line, "#") {
			urls = append(urls, line)
		}
	}
	return urls, sc.Err()
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := in[:0:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func (a *app) wait(ctx context.Context, d time.Duration) error {
	if a.sleep != nil {
		return a.sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

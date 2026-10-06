package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
	"github.com/spf13/cobra"
)

// readOnlyPOST lists POST paths that only read data.
var readOnlyPOST = []string{"/searchAnalytics/query", "/v1/urlInspection/index:inspect"}

// apiIsRead classifies a raw request by effect, not HTTP method.
func apiIsRead(method, path string) bool {
	if method == http.MethodGet {
		return true
	}
	if method == http.MethodPost {
		p := strings.SplitN(path, "?", 2)[0]
		for _, suffix := range readOnlyPOST {
			if strings.HasSuffix(p, suffix) {
				return true
			}
		}
	}
	return false
}

func (a *app) api() *cobra.Command {
	var data string
	c := &cobra.Command{Use: "api METHOD PATH", Short: "Send an authenticated request to the Search Console API (escape hatch)", Args: cobra.ExactArgs(2),
		Long: "PATH is relative to https://searchconsole.googleapis.com and must already be URL-escaped.\n" +
			"GET and the read-only POSTs (searchAnalytics/query, urlInspection/index:inspect) are reads; every other\n" +
			"request is treated as a write: blocked by --read-only, printed instead of sent with --dry-run, and\n" +
			"confirmed interactively (or with --yes) before it is sent.",
		Example: "  gsc api GET /webmasters/v3/sites\n" +
			"  gsc api POST /webmasters/v3/sites/sc-domain%3Aexample.com/searchAnalytics/query --data '{\"startDate\":\"2026-09-01\",\"endDate\":\"2026-09-30\"}'\n" +
			"  gsc api PUT /webmasters/v3/sites/sc-domain%3Aexample.com/sitemaps/https%3A%2F%2Fexample.com%2Fsitemap.xml --dry-run",
		Annotations: map[string]string{annMutates: "conditional", annInteractive: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			method := strings.ToUpper(args[0])
			switch method {
			case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			default:
				return withKind(kindUsage, fmt.Errorf("unsupported method %q", args[0]))
			}
			path := args[1]
			if !strings.HasPrefix(path, "/") || strings.Contains(path, "://") {
				return withKind(kindUsage, errors.New("PATH must start with / and be relative to the API host"))
			}
			var body []byte
			if data != "" {
				var err error
				switch {
				case data == "-":
					body, err = readAllLimited(a.reader())
				case strings.HasPrefix(data, "@"):
					body, err = os.ReadFile(data[1:])
				default:
					body = []byte(data)
				}
				if err != nil {
					return err
				}
				if !json.Valid(body) {
					return errors.New("--data is not valid JSON")
				}
			}
			read := apiIsRead(method, path)
			if !read && a.readOnly {
				return withKind(kindReadOnly, fmt.Errorf("%s %s is a write and is blocked by --read-only", method, client.SafeURL(client.DefaultBase+path)))
			}
			if !read && a.dryRun {
				var shown any
				if body != nil {
					shown = json.RawMessage(body)
				}
				return a.dryRunPlan(method, path, shown)
			}
			// A raw write can delete properties or sitemaps, so it confirms like the
			// typed destructive commands (or needs --yes).
			if !read {
				if err := a.confirm(fmt.Sprintf("gsc api %s %s changes Search Console data.", method, client.SafeURL(client.DefaultBase+path))); err != nil {
					return err
				}
			}
			cl, cr, err := a.connect(cmd.Context())
			if err != nil {
				return err
			}
			if !read {
				if err := requireWriteScope(cr); err != nil {
					return err
				}
			}
			var payload any
			if body != nil {
				payload = body
			}
			resp, err := cl.Do(cmd.Context(), method, path, nil, payload)
			if err != nil {
				return err
			}
			if len(strings.TrimSpace(string(resp))) == 0 {
				return a.print(map[string]any{"ok": true})
			}
			if a.format == "json" || a.format == "table" {
				return a.printRaw(resp)
			}
			var v any
			if err := json.Unmarshal(resp, &v); err != nil {
				return a.printRaw(resp)
			}
			return a.print(v)
		}}
	c.Flags().StringVar(&data, "data", "", "JSON request body, @file, or - for stdin")
	c.AddCommand(a.apiMethods())
	return c
}

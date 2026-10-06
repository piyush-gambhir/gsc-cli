package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/config"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/output"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/site"
	"github.com/spf13/cobra"
)

func (a *app) sites() *cobra.Command {
	c := &cobra.Command{Use: "sites", Short: "List and manage Search Console properties"}
	c.AddCommand(a.sitesList(), a.sitesGet(), a.sitesUse(), a.sitesAdd(), a.sitesRemove())
	return c
}

func (a *app) sitesList() *cobra.Command {
	return &cobra.Command{Use: "list", Short: "List properties you can access and your permission level", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		c, _, err := a.connect(cmd.Context())
		if err != nil {
			return err
		}
		sites, err := c.ListSites(cmd.Context())
		if err != nil {
			return err
		}
		sort.Slice(sites, func(i, j int) bool { return sites[i].SiteURL < sites[j].SiteURL })
		return a.print(sites)
	}}
}

func (a *app) sitesGet() *cobra.Command {
	return &cobra.Command{Use: "get [SITE]", Short: "Show one property (default: the profile's site)", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		c, _, err := a.connect(cmd.Context())
		if err != nil {
			return err
		}
		s, err := a.resolveSite(cmd.Context(), c, firstArg(args))
		if err != nil {
			return err
		}
		entry, err := c.GetSite(cmd.Context(), s)
		if err != nil {
			return err
		}
		return a.print(entry)
	}}
}

func firstArg(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

func (a *app) sitesUse() *cobra.Command {
	return &cobra.Command{Use: "use SITE", Short: "Set the profile's default site", Args: cobra.ExactArgs(1),
		Annotations: map[string]string{annWritesLocal: "true"},
		Long:        "Exact property identifiers are saved without a network call; a bare host is resolved with one sites.list call.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := a.loadConfig()
			if err != nil {
				return err
			}
			name := cfg.SelectProfile(a.profile)
			if _, ok := cfg.Profiles[name]; !ok {
				return errors.New("no profile to update; run gsc auth login first")
			}
			chosen := args[0]
			if !site.Exact(chosen) {
				c, _, err := a.connect(cmd.Context())
				if err != nil {
					return err
				}
				if chosen, err = a.resolveSite(cmd.Context(), c, chosen); err != nil {
					return err
				}
			}
			if err := config.Update(cmd.Context(), path, func(c *config.Config) error {
				p, ok := c.Profiles[name]
				if !ok {
					return fmt.Errorf("profile %q not found", name)
				}
				p.Site = chosen
				c.Profiles[name] = p
				return nil
			}); err != nil {
				return err
			}
			return a.print(map[string]string{"profile": name, "site": chosen})
		}}
}

// dryRunPlan prints the request a write command would send.
func (a *app) dryRunPlan(method, path string, body any) error {
	plan := map[string]any{"dry_run": true, "method": method, "url": client.SafeURL(client.DefaultBase + path)}
	if body != nil {
		plan["body"] = body
	}
	return a.print(plan)
}

func (a *app) sitesAdd() *cobra.Command {
	return &cobra.Command{Use: "add SITE", Short: "Add a property to your Search Console list (does not verify ownership)", Args: cobra.ExactArgs(1),
		Long: "SITE must be exact: sc-domain:example.com or a URL-prefix such as https://www.example.com/.\n" +
			"Adding a property does not verify it. A URL-prefix inside a domain property you already own is verified\n" +
			"at once; otherwise complete verification in Search Console. The output reports the resulting permission.",
		Annotations: map[string]string{annMutates: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if !site.Exact(args[0]) {
				return fmt.Errorf("use an exact property: sc-domain:%s or https://%s/", args[0], args[0])
			}
			if a.dryRun {
				return a.dryRunPlan(http.MethodPut, "/webmasters/v3/sites/"+client.EscapeSegment(args[0]), nil)
			}
			c, cr, err := a.connect(cmd.Context())
			if err != nil {
				return err
			}
			if err := requireWriteScope(cr); err != nil {
				return err
			}
			if err := c.AddSite(cmd.Context(), args[0]); err != nil {
				return err
			}
			// Read back what Google granted: a URL-prefix under an owned domain property is verified at once.
			out := map[string]any{"site": args[0], "added": true}
			if e, err := c.GetSite(cmd.Context(), args[0]); err == nil {
				out["permissionLevel"] = e.PermissionLevel
				out["verified"] = e.PermissionLevel != "siteUnverifiedUser"
			}
			return a.print(out)
		}}
}

func (a *app) sitesRemove() *cobra.Command {
	return &cobra.Command{Use: "remove SITE", Short: "Remove a property from your Search Console list", Args: cobra.ExactArgs(1),
		Annotations: map[string]string{annMutates: "true", annInteractive: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if !site.Exact(args[0]) {
				return fmt.Errorf("use the exact property identifier shown by gsc sites list")
			}
			if a.dryRun {
				return a.dryRunPlan(http.MethodDelete, "/webmasters/v3/sites/"+client.EscapeSegment(args[0]), nil)
			}
			if err := a.confirm(fmt.Sprintf("Remove %s from your Search Console properties?", args[0])); err != nil {
				return err
			}
			c, cr, err := a.connect(cmd.Context())
			if err != nil {
				return err
			}
			if err := requireWriteScope(cr); err != nil {
				return err
			}
			if err := c.DeleteSite(cmd.Context(), args[0]); err != nil {
				return err
			}
			return a.print(map[string]any{"site": args[0], "removed": true})
		}}
}

func (a *app) sitemaps() *cobra.Command {
	c := &cobra.Command{Use: "sitemaps", Short: "List, inspect, submit, and delete sitemaps for a property"}
	var index string
	list := &cobra.Command{Use: "list", Short: "List submitted sitemaps", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		cl, _, err := a.connect(cmd.Context())
		if err != nil {
			return err
		}
		s, err := a.resolveSite(cmd.Context(), cl, "")
		if err != nil {
			return err
		}
		maps, err := cl.ListSitemaps(cmd.Context(), s, index)
		if err != nil {
			return err
		}
		return a.print(sitemapList(maps))
	}}
	list.Flags().StringVar(&index, "index", "", "Only sitemaps listed in this sitemap index URL")
	get := &cobra.Command{Use: "get SITEMAP_URL", Short: "Show one sitemap's status, errors, and warnings", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cl, _, err := a.connect(cmd.Context())
		if err != nil {
			return err
		}
		s, err := a.resolveSite(cmd.Context(), cl, "")
		if err != nil {
			return err
		}
		m, err := cl.GetSitemap(cmd.Context(), s, args[0])
		if err != nil {
			return err
		}
		return a.print(m)
	}}
	submit := &cobra.Command{Use: "submit SITEMAP_URL", Short: "Submit a sitemap URL for the property", Args: cobra.ExactArgs(1),
		Annotations: map[string]string{annMutates: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.sitemapWrite(cmd, args[0], http.MethodPut)
		}}
	del := &cobra.Command{Use: "delete SITEMAP_URL", Short: "Delete a sitemap from the report (Google may still crawl it)", Args: cobra.ExactArgs(1),
		Annotations: map[string]string{annMutates: "true", annInteractive: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.sitemapWrite(cmd, args[0], http.MethodDelete)
		}}
	c.AddCommand(list, get, submit, del)
	return c
}

func (a *app) sitemapWrite(cmd *cobra.Command, feed, method string) error {
	ctx := cmd.Context()
	input := a.siteInput()
	if a.dryRun && site.Exact(input) {
		return a.dryRunPlan(method, "/webmasters/v3/sites/"+client.EscapeSegment(input)+"/sitemaps/"+client.EscapeSegment(feed), nil)
	}
	cl, cr, err := a.connect(ctx)
	if err != nil {
		return err
	}
	s, err := a.resolveSite(ctx, cl, "")
	if err != nil {
		return err
	}
	if a.dryRun {
		return a.dryRunPlan(method, "/webmasters/v3/sites/"+client.EscapeSegment(s)+"/sitemaps/"+client.EscapeSegment(feed), nil)
	}
	if err := requireWriteScope(cr); err != nil {
		return err
	}
	if method == http.MethodDelete {
		if err := a.confirm(fmt.Sprintf("Delete sitemap %s from %s? (This removes the report entry; it does not stop crawling.)", feed, s)); err != nil {
			return err
		}
		if err := cl.DeleteSitemap(ctx, s, feed); err != nil {
			return err
		}
		return a.print(map[string]any{"site": s, "sitemap": feed, "deleted": true})
	}
	if err := cl.SubmitSitemap(ctx, s, feed); err != nil {
		return err
	}
	return a.print(map[string]any{"site": s, "sitemap": feed, "submitted": true})
}

// sitemapRows keeps the API objects for JSON/YAML and a focused table.
type sitemapRows []map[string]any

func sitemapList(m []map[string]any) sitemapRows { return sitemapRows(m) }

func (r sitemapRows) Table() output.Table {
	cols := []string{"path", "type", "isPending", "isSitemapsIndex", "lastSubmitted", "lastDownloaded", "warnings", "errors"}
	t := output.Table{Columns: cols, Human: map[string]func(any) string{"lastSubmitted": output.Timestamp, "lastDownloaded": output.Timestamp},
		HumanColumns: []string{"path", "type", "isPending", "lastSubmitted", "lastDownloaded", "warnings", "errors"}}
	for _, m := range r {
		row := make([]any, len(cols))
		for i, c := range cols {
			row[i] = m[c]
		}
		t.Rows = append(t.Rows, row)
	}
	return t
}

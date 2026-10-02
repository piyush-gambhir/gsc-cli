package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/auth"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/config"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/output"
	"github.com/spf13/cobra"
)

type check struct {
	Check  string `json:"check"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type checks []check

func (c checks) Table() output.Table {
	t := output.Table{Columns: []string{"check", "status", "detail"}}
	for _, x := range c {
		t.Rows = append(t.Rows, []any{x.Check, x.Status, x.Detail})
	}
	return t
}

func (a *app) doctor() *cobra.Command {
	var online bool
	c := &cobra.Command{Use: "doctor", Short: "Check configuration, credentials, and PATH (local unless --online)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var results checks
			add := func(name, status, detail string, args ...any) {
				results = append(results, check{name, status, fmt.Sprintf(detail, args...)})
			}
			cfg, path, err := a.loadConfig()
			if err != nil {
				add("config", "fail", "%v", err)
			} else {
				add("config", "ok", "%s (%d %s)", path, len(cfg.Profiles), plural(len(cfg.Profiles), "profile", "profiles"))
			}
			if auth.BuiltinClientID != "" {
				add("oauth client", "ok", "built-in client present")
			} else if os.Getenv("GSC_CLIENT_ID") != "" {
				add("oauth client", "ok", "using GSC_CLIENT_ID")
			} else {
				add("oauth client", "warn", "this build has no built-in OAuth client; browser login needs GSC_CLIENT_ID/GSC_CLIENT_SECRET or --client-secret-file")
			}
			if cfg != nil {
				name := cfg.SelectProfile(a.profile)
				p, ok := cfg.Profiles[name]
				switch {
				case name == "":
					add("profile", "warn", "no profile selected; run gsc auth login")
				case !ok:
					add("profile", "fail", "profile %q not found", name)
				default:
					add("profile", "ok", "%s (%s%s)", name, p.Auth, map[bool]string{true: ", " + p.Account, false: ""}[p.Account != ""])
					if p.Site == "" {
						add("default site", "warn", "none; run gsc sites use SITE")
					} else {
						add("default site", "ok", "%s", p.Site)
					}
					if p.Auth == config.AuthOAuth {
						store, err := a.store()
						if err == nil {
							_, err = auth.LoadBlob(store, storeOf(p), name)
						}
						if err != nil {
							add("token store", "fail", "%v", auth.DescribeStoreError(err, name))
						} else {
							add("token store", map[bool]string{true: "warn", false: "ok"}[p.TokenStore == config.StoreFile], "%s", p.TokenStore)
						}
						if !auth.HasWriteScope(p.Scopes) {
							add("scope", "warn", "read-only scope; write commands need gsc auth login without --scope readonly")
						}
					}
					if p.Auth == config.AuthServiceAccount {
						if _, err := os.Stat(p.KeyFile); err != nil {
							add("key file", "fail", "%v", err)
						} else {
							add("key file", "ok", "%s", p.KeyFile)
						}
					}
				}
			}
			if online {
				cl, cr, err := a.connect(cmd.Context())
				if err == nil {
					var n int
					sites, lerr := cl.ListSites(cmd.Context())
					if lerr != nil {
						err = lerr
					} else {
						n = len(sites)
						add("api", "ok", "%s can list %d %s", cr.source, n, plural(n, "property", "properties"))
					}
				}
				if err != nil {
					add("api", "fail", "%v", err)
				}
			}
			for _, other := range otherBinaries("gsc") {
				add("PATH", "warn", "another gsc is on PATH at %s (Ghostscript and Gambit Scheme also install gsc); call this binary by full path or reorder PATH", other)
			}
			if err := a.print(results); err != nil {
				return err
			}
			for _, c := range results {
				if c.Status == "fail" {
					return fmt.Errorf("doctor found problems")
				}
			}
			return nil
		}}
	c.Flags().BoolVar(&online, "online", false, "Also call the API once to list properties")
	return c
}

// otherBinaries lists executables named name on PATH other than this one.
func otherBinaries(name string) []string {
	self, _ := os.Executable()
	self, _ = filepath.EvalSymlinks(self)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	var out []string
	seen := map[string]bool{}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		p := filepath.Join(dir, name)
		info, err := os.Stat(p)
		if err != nil || info.IsDir() || (runtime.GOOS != "windows" && info.Mode()&0o111 == 0) {
			continue
		}
		real, _ := filepath.EvalSymlinks(p)
		if real == "" {
			real = p
		}
		if strings.EqualFold(real, self) || seen[real] {
			continue
		}
		seen[real] = true
		out = append(out, p)
	}
	return out
}

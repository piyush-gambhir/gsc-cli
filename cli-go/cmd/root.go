package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/analytics"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/auth"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/build"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/config"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/output"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/secrets"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/site"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/update"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type app struct {
	format, profile, site, accessToken, credentials string
	timeout                                         time.Duration
	noInput, quiet, verbose, readOnly, dryRun, yes  bool
	in                                              io.Reader
	out, errOut                                     io.Writer

	transport   http.RoundTripper
	provider    func(*http.Client) *auth.Provider
	apiBase     string
	openBrowser func(string) error
	now         func() time.Time
	terminal    func() bool
	sleep       func(context.Context, time.Duration) error
	exePath     string
	stderrTTY   func() bool

	stdin *bufio.Reader

	updateCheck  chan update.Cache
	checks       sync.WaitGroup
	checkStarted bool
}

const (
	annMutates     = "mutates"
	annWritesLocal = "writes-local"
	annInteractive = "interactive"
	annSecret      = "secret-output"
	annGroup       = "group"
)

func envBool(name string) bool { s := os.Getenv(name); return s == "1" || strings.EqualFold(s, "true") }

func NewRoot(in io.Reader, out, errOut io.Writer) *cobra.Command {
	return newRoot(newApp(in, out, errOut))
}

func newApp(in io.Reader, out, errOut io.Writer) *app {
	return &app{in: in, out: out, errOut: errOut, provider: auth.DefaultProvider, now: time.Now}
}

func newRoot(a *app) *cobra.Command {
	root := &cobra.Command{
		Use:   "gsc",
		Short: "Google Search Console from your terminal",
		Long: "Search performance, URL inspection, sitemaps, and properties from Google Search Console.\n" +
			"Run `gsc auth login` once; it opens your browser and remembers the login.\n" +
			"Data goes to stdout and diagnostics to stderr. --read-only blocks every write.",
		SilenceUsage: true, SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if !output.Valid(a.format) {
				return withKind(kindUsage, fmt.Errorf("unsupported output %q; use table, json, yaml, or csv", a.format))
			}
			if a.timeout <= 0 {
				return withKind(kindUsage, fmt.Errorf("--timeout must be positive"))
			}
			if envBool("GSC_READ_ONLY") {
				a.readOnly = true
			}
			if envBool("GSC_NO_INPUT") {
				a.noInput = true
			}
			if a.readOnly {
				if cmd.Annotations[annSecret] == "true" {
					return withKind(kindReadOnly, fmt.Errorf("%s prints a credential and is blocked by --read-only", cmd.CommandPath()))
				}
				if cmd.Annotations[annMutates] == "true" {
					return withKind(kindReadOnly, fmt.Errorf("%s changes Search Console data and is blocked by --read-only", cmd.CommandPath()))
				}
				if cmd.Annotations[annWritesLocal] == "true" {
					return withKind(kindReadOnly, fmt.Errorf("%s changes local state and is blocked by --read-only", cmd.CommandPath()))
				}
			}
			if a.dryRun && cmd.Annotations[annWritesLocal] == "true" {
				return withKind(kindUsage, fmt.Errorf("%s has no --dry-run preview; run it without --dry-run", cmd.CommandPath()))
			}
			a.startUpdateCheck(cmd)
			return nil
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) { a.printUpdateNotice() },
	}
	root.SetIn(a.in)
	root.SetOut(a.out)
	root.SetErr(a.errOut)
	f := root.PersistentFlags()
	f.StringVarP(&a.format, "output", "o", "table", "Output format: table, json, yaml, csv")
	f.StringVar(&a.profile, "profile", "", "Named profile (or GSC_PROFILE)")
	f.StringVarP(&a.site, "site", "s", "", "Property: sc-domain:example.com, https://www.example.com/, or a bare host (or GSC_SITE)")
	f.StringVar(&a.accessToken, "access-token", "", "Use this OAuth access token (prefer GSC_ACCESS_TOKEN)")
	f.StringVar(&a.credentials, "credentials", "", "Google credentials JSON: service account, authorized user, or external account (or GSC_CREDENTIALS)")
	f.DurationVar(&a.timeout, "timeout", 30*time.Second, "HTTP request timeout")
	f.BoolVar(&a.noInput, "no-input", envBool("GSC_NO_INPUT"), "Never prompt or open a browser")
	f.BoolVarP(&a.quiet, "quiet", "q", envBool("GSC_QUIET"), "Suppress informational stderr output")
	f.BoolVarP(&a.verbose, "verbose", "v", envBool("GSC_VERBOSE"), "Log request method, URL, and status to stderr (never tokens or bodies)")
	f.BoolVar(&a.readOnly, "read-only", envBool("GSC_READ_ONLY"), "Block remote writes, local credential changes, and self-update")
	f.BoolVar(&a.dryRun, "dry-run", false, "For write commands: print the request and send nothing")
	f.BoolVarP(&a.yes, "yes", "y", false, "Confirm destructive commands and updates without prompting")

	root.AddCommand(a.auth(), a.configCmd(), a.sites(), a.sitemaps(), a.query(), a.performance(), a.top(), a.trend(),
		a.freshness(), a.inspect(), a.export(), a.insights(), a.api(), a.doctor(), a.update(), a.commandsCmd())
	login := a.login()
	login.Use = "login"
	login.Short = "Alias of auth login"
	login.Example = "  gsc login\n  gsc login --no-browser\n  gsc login --profile work --scope readonly"
	status := a.status()
	status.Use = "status"
	status.Short = "Alias of auth status"
	root.AddCommand(login, status)
	root.AddCommand(&cobra.Command{Use: "version", Short: "Print build information", Long: "Prints the version, commit, build date, and whether a built-in OAuth client is present. latest and\nupdate_available come from the last release check (see gsc update --help) and appear only when one is\ncached; version never uses the network.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		info := map[string]any{"version": build.Version, "commit": build.Commit, "date": build.Date, "builtin_oauth_client": auth.BuiltinClientID != ""}
		if dir, err := config.Dir(); err == nil {
			if c := update.ReadCache(dir); c.LatestVersion != "" {
				info["latest"] = c.LatestVersion
				info["update_available"] = update.Newer(c.LatestVersion, build.Version)
			}
		}
		return a.print(info)
	}})
	root.AddCommand(&cobra.Command{Use: "completion [bash|zsh|fish|powershell]", Short: "Generate shell completion script", Args: cobra.ExactArgs(1), ValidArgs: []string{"bash", "zsh", "fish", "powershell"}, RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return root.GenBashCompletion(a.out)
		case "zsh":
			return root.GenZshCompletion(a.out)
		case "fish":
			return root.GenFishCompletion(a.out, true)
		case "powershell":
			return root.GenPowerShellCompletionWithDesc(a.out)
		}
		return withKind(kindUsage, fmt.Errorf("unsupported shell %q", args[0]))
	}})
	root.CompletionOptions.DisableDefaultCmd = true
	rejectUnknownSubcommands(root)
	addExamples(root)
	initHelpFlags(root)
	return root
}

func initHelpFlags(c *cobra.Command) {
	c.InitDefaultHelpFlag()
	c.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return withKind(kindUsage, err) })
	if args := c.Args; args != nil {
		c.Args = func(cmd *cobra.Command, a []string) error { return withKind(kindUsage, args(cmd, a)) }
	}
	for _, sub := range c.Commands() {
		initHelpFlags(sub)
	}
}

func isGroup(c *cobra.Command) bool { return c.Annotations[annGroup] == "true" }

func rejectUnknownSubcommands(c *cobra.Command) {
	for _, sub := range c.Commands() {
		rejectUnknownSubcommands(sub)
	}
	if !c.HasParent() || !c.HasSubCommands() || c.Runnable() {
		return
	}
	if c.Annotations == nil {
		c.Annotations = map[string]string{}
	}
	c.Annotations[annGroup] = "true"
	c.Args = cobra.ArbitraryArgs
	c.SuggestionsMinimumDistance = 2
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		msg := fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath())
		if s := cmd.SuggestionsFor(args[0]); len(s) > 0 {
			msg += "\n\nDid you mean this?\n\t" + strings.Join(s, "\n\t")
		}
		return withKind(kindUsage, errors.New(msg))
	}
}

func outputFlag(args []string) string {
	f := ""
	for i := 0; i < len(args); i++ {
		switch s := args[i]; {
		case s == "--":
			return f
		case s == "-o" || s == "--output":
			if i+1 < len(args) {
				f, i = args[i+1], i+1
			}
		case strings.HasPrefix(s, "--output="):
			f = strings.TrimPrefix(s, "--output=")
		case strings.HasPrefix(s, "-o"):
			f = strings.TrimPrefix(strings.TrimPrefix(s, "-o"), "=")
		}
	}
	return f
}

func Run(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
	if runtime.GOOS == "windows" {
		if exe, err := os.Executable(); err == nil {
			if exe, err = filepath.EvalSymlinks(exe); err == nil {
				update.RemoveLeftover(exe)
			}
		}
	}
	a := newApp(in, out, errOut)
	return a.run(ctx, args)
}

func (a *app) run(ctx context.Context, args []string) int {
	root := newRoot(a)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	message := strings.TrimSpace(err.Error())
	for _, secret := range []string{a.accessToken, os.Getenv("GSC_ACCESS_TOKEN"), os.Getenv("GSC_CLIENT_SECRET"), auth.BuiltinClientSecret} {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[REDACTED]")
		}
	}
	payload := map[string]any{"error": message, "kind": errorKind(err)}
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		payload["status"] = apiErr.Status
		if apiErr.Reason != "" {
			payload["reason"] = apiErr.Reason
		}
		if apiErr.RetryAfter != "" {
			payload["retry_after"] = apiErr.RetryAfter
		}
	}
	if o := root.PersistentFlags().Lookup("output"); o != nil && !o.Changed {
		if f := outputFlag(args); f == "json" || f == "yaml" {
			a.format = f
		}
	}
	if a.format == "json" || a.format == "yaml" {
		_ = output.Print(a.errOut, a.format, payload)
	} else {
		fmt.Fprintln(a.errOut, "Error:", message)
	}
	a.printUpdateNotice()
	return 1
}

func (a *app) print(data any) error {
	r, ok := data.(*analytics.Result)
	if c, isCmp := data.(*analytics.Comparison); isCmp {
		r, ok = c.Current, true
	}
	if ok && r != nil && a.format != "json" && a.format != "yaml" {
		switch at := r.FirstIncompleteDate + r.FirstIncompleteHour; {
		case at != "":
			a.warn("data from %s on is preliminary and may change; JSON and YAML output label it", at)
		case r.Request.DataState == "all" || r.Request.DataState == "hourly_all":
			a.warn("--data-state %s may include preliminary data that can change", r.Request.DataState)
		}
	}
	return output.Print(a.out, a.format, data)
}

func (a *app) info(format string, args ...any) {
	if !a.quiet {
		fmt.Fprintf(a.errOut, format+"\n", args...)
	}
}

func (a *app) warn(format string, args ...any) {
	fmt.Fprintf(a.errOut, "Warning: "+format+"\n", args...)
}

func (a *app) httpClient() *http.Client {
	return &http.Client{Timeout: a.timeout, Transport: a.transport, CheckRedirect: client.NoRedirects}
}

func (a *app) prov() *auth.Provider { return a.provider(a.httpClient()) }

func (a *app) store() (secrets.Store, error) {
	path, err := config.Path()
	if err != nil {
		return secrets.Store{}, err
	}
	def, _ := config.DefaultPath()
	return secrets.Store{Service: secrets.ServiceFor("gsc-cli", path, def), FilePath: filepath.Join(filepath.Dir(path), "secrets.yaml")}, nil
}

func (a *app) loadConfig() (*config.Config, string, error) {
	p, err := config.Path()
	if err != nil {
		return nil, "", err
	}
	c, err := config.Load(p)
	return c, p, err
}

type creds struct {
	ts          client.TokenSource
	source      string
	profileName string
	profile     *config.Profile
}

func (a *app) scopes() []string {
	if a.readOnly {
		return []string{auth.ScopeReadonly}
	}
	return []string{auth.ScopeFull}
}

func (a *app) profileScopes(p config.Profile) []string {
	if len(p.Scopes) > 0 && !auth.HasWriteScope(p.Scopes) {
		return []string{auth.ScopeReadonly}
	}
	return a.scopes()
}

func (a *app) resolveCreds(ctx context.Context) (*creds, error) {
	if a.accessToken != "" {
		return &creds{ts: auth.Static(strings.TrimSpace(a.accessToken)), source: "flag"}, nil
	}
	if t := os.Getenv("GSC_ACCESS_TOKEN"); t != "" {
		return &creds{ts: auth.Static(strings.TrimSpace(t)), source: "env:GSC_ACCESS_TOKEN"}, nil
	}
	credFile, credSource := a.credentials, "flag"
	if credFile == "" {
		credFile, credSource = os.Getenv("GSC_CREDENTIALS"), "env:GSC_CREDENTIALS"
	}
	if credFile != "" {
		ts, kind, err := a.prov().CredentialsFile(ctx, credFile, a.scopes())
		if err != nil {
			return nil, err
		}
		return &creds{ts: ts, source: credSource + ":" + kind}, nil
	}
	cfg, path, err := a.loadConfig()
	if err != nil {
		return nil, err
	}
	name := cfg.SelectProfile(a.profile)
	if name == "" {
		return nil, withKind(kindAuth, errors.New("not logged in; run gsc auth login (or set GSC_ACCESS_TOKEN or GSC_CREDENTIALS)"))
	}
	p, ok := cfg.Profiles[name]
	if !ok {
		return nil, withKind(kindAuth, fmt.Errorf("profile %q not found; run gsc auth login --profile %s", name, name))
	}
	c := &creds{source: "profile:" + name, profileName: name, profile: &p}
	switch p.Auth {
	case config.AuthOAuth:
		store, err := a.store()
		if err != nil {
			return nil, err
		}
		c.ts = &auth.Session{Provider: a.prov(), Store: store, ConfigPath: path, ProfileName: name, Profile: p, ReadOnly: a.readOnly}
	case config.AuthServiceAccount:
		ts, _, err := a.prov().ServiceAccount(ctx, p.KeyFile, p.Subject, a.profileScopes(p))
		if err != nil {
			return nil, err
		}
		c.ts = ts
	case config.AuthADC:
		prov := a.prov()
		if p.Impersonate != "" {
			base, err := prov.ADC(ctx, auth.BaseForImpersonation())
			if err != nil {
				return nil, err
			}
			c.ts = prov.Impersonate(ctx, base.TS, p.Impersonate, a.profileScopes(p))
		} else {
			ts, err := prov.ADC(ctx, a.profileScopes(p))
			if err != nil {
				return nil, err
			}
			c.ts = ts
		}
	default:
		return nil, fmt.Errorf("profile %q has unknown auth type %q", name, p.Auth)
	}
	return c, nil
}

func (a *app) newClient(ts client.TokenSource) *client.Client {
	c := client.New(a.httpClient(), ts)
	if a.apiBase != "" {
		c.Base = a.apiBase
	}
	if a.verbose {
		c.Log = a.errOut
	}
	return c
}

func (a *app) connect(ctx context.Context) (*client.Client, *creds, error) {
	c, err := a.resolveCreds(ctx)
	if err != nil {
		return nil, nil, err
	}
	return a.newClient(c.ts), c, nil
}

func (a *app) siteInput() string {
	if a.site != "" {
		return a.site
	}
	if s := os.Getenv("GSC_SITE"); s != "" {
		return s
	}
	if cfg, _, err := a.loadConfig(); err == nil {
		if p, ok := cfg.Profiles[cfg.SelectProfile(a.profile)]; ok {
			return p.Site
		}
	}
	return ""
}

func (a *app) resolveSite(ctx context.Context, c *client.Client, explicit string) (string, error) {
	input := explicit
	if input == "" {
		input = a.siteInput()
	}
	return site.Resolve(ctx, input, c.ListSites)
}

func requireWriteScope(c *creds) error {
	if c.profile != nil && len(c.profile.Scopes) > 0 && !auth.HasWriteScope(c.profile.Scopes) {
		return withKind(kindReadOnly, fmt.Errorf("profile %q has read-only Search Console access; run gsc auth login --profile %s (without --scope readonly) to allow writes", c.profileName, c.profileName))
	}
	return nil
}

func (a *app) isTerminal() bool {
	if a.terminal != nil {
		return a.terminal()
	}
	f, ok := a.in.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func (a *app) reader() *bufio.Reader {
	if a.stdin == nil {
		a.stdin = bufio.NewReader(a.in)
	}
	return a.stdin
}

func (a *app) confirm(question string) error {
	if a.yes {
		return nil
	}
	if a.noInput || !a.isTerminal() {
		return withKind(kindConfirmation, fmt.Errorf("%s Refusing without --yes", question))
	}
	fmt.Fprintf(a.errOut, "%s [y/N]: ", question)
	line, _ := a.reader().ReadString('\n')
	if s := strings.ToLower(strings.TrimSpace(line)); s == "y" || s == "yes" {
		return nil
	}
	return errors.New("cancelled")
}

func (a *app) today() analytics.Date { return analytics.DateOf(a.now()) }

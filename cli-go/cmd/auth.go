package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/auth"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/config"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/secrets"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/site"
	"github.com/spf13/cobra"
)

func (a *app) auth() *cobra.Command {
	c := &cobra.Command{Use: "auth", Short: "Log in, inspect credentials, and manage profiles"}
	c.AddCommand(a.login(), a.status(), a.listProfiles(), a.useProfile(), a.logout(), a.tokenCmd())
	return c
}

// loginProfile is --profile, then GSC_PROFILE, then the current profile, then "default".
// An unreadable config fails here, before the browser opens or a token is
// stored, rather than silently falling back to "default".
func (a *app) loginProfile() (string, error) {
	cfg, _, err := a.loadConfig()
	if err != nil {
		return "", err
	}
	name := cfg.SelectProfile(a.profile)
	if name == "" {
		name = "default"
	}
	return name, config.ValidName(name)
}

type loginFlags struct {
	scope, clientSecretFile, serviceAccount, subject, impersonate string
	noBrowser, adc, insecureStorage, noVerify                     bool
}

func (a *app) login() *cobra.Command {
	var f loginFlags
	c := &cobra.Command{Use: "login", Short: "Sign in with Google (opens your browser) or save another credential type", Args: cobra.NoArgs,
		Long: "With no flags, opens Google's consent page in your browser and saves the login in the OS keychain.\n" +
			"Tokens refresh automatically afterwards. --no-browser prints a URL to approve on any device and\n" +
			"asks you to paste the final redirected URL back. --service-account and --adc save non-interactive\n" +
			"credentials for CI. See docs/auth.md for every method.",
		Example: "  gsc auth login\n  gsc auth login --profile work --scope readonly\n  gsc auth login --no-browser\n" +
			"  gsc auth login --client-secret-file client_secret.json\n  gsc auth login --profile ci --service-account key.json\n" +
			"  gsc auth login --profile gcloud --adc --impersonate reporting@my-project.iam.gserviceaccount.com",
		Annotations: map[string]string{annWritesLocal: "true", annInteractive: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := a.loginProfile()
			if err != nil {
				return err
			}
			if f.serviceAccount != "" && f.adc {
				return errors.New("--service-account and --adc are mutually exclusive")
			}
			if f.subject != "" && f.serviceAccount == "" {
				return errors.New("--subject requires --service-account")
			}
			if f.impersonate != "" && !f.adc {
				return errors.New("--impersonate requires --adc")
			}
			scope := auth.ScopeFull
			switch f.scope {
			case "full":
			case "readonly":
				scope = auth.ScopeReadonly
			default:
				return fmt.Errorf("--scope must be full or readonly")
			}
			switch {
			case f.serviceAccount != "":
				return a.loginServiceAccount(cmd.Context(), name, f, scope)
			case f.adc:
				return a.loginADC(cmd.Context(), name, f, scope)
			}
			return a.loginOAuth(cmd.Context(), name, f, scope)
		},
	}
	fl := c.Flags()
	fl.StringVar(&f.scope, "scope", "full", "Search Console access to request: full or readonly")
	fl.BoolVar(&f.noBrowser, "no-browser", false, "Headless login: approve on any device, then paste the redirected URL")
	fl.StringVar(&f.clientSecretFile, "client-secret-file", "", "Use your own Desktop OAuth client (JSON from Google Cloud Console)")
	fl.StringVar(&f.serviceAccount, "service-account", "", "Save a service account JSON key file for this profile")
	fl.StringVar(&f.subject, "subject", "", "With --service-account: Workspace user to impersonate (domain-wide delegation)")
	fl.BoolVar(&f.adc, "adc", false, "Use Application Default Credentials (gcloud, workload identity federation, metadata server)")
	fl.StringVar(&f.impersonate, "impersonate", "", "With --adc: service account email to impersonate through IAM Credentials")
	fl.BoolVar(&f.insecureStorage, "insecure-storage", false, "Store the login in a 0600 plaintext file instead of the OS keychain")
	fl.BoolVar(&f.noVerify, "no-verify", false, "Skip listing properties after login (no API call)")
	return c
}

func randomGeneration() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (a *app) loginOAuth(ctx context.Context, name string, f loginFlags, scope string) error {
	if a.noInput {
		return withKind(kindAuth, errors.New("browser login needs interaction and --no-input is set; for automation use GSC_ACCESS_TOKEN, GSC_CREDENTIALS, or gsc auth login --service-account KEY.json"))
	}
	var cl auth.Client
	var err error
	if f.clientSecretFile != "" {
		cl, err = auth.ClientFromSecretFile(f.clientSecretFile)
	} else {
		cl, err = auth.ResolveClient()
	}
	if err != nil {
		return err
	}
	store, err := a.store()
	if err != nil {
		return err
	}
	cfgPath, err := config.Path()
	if err != nil {
		return err
	}
	// Hold the profile lock only while exchanging and saving, so the follow-up
	// sites.list (which may refresh) never waits on our own lock.
	var res *auth.LoginResult
	var saved secrets.Saved
	var p config.Profile
	err = func() error {
		opts := auth.LoginOptions{Client: cl, Scopes: []string{scope}, OpenBrowser: a.openBrowser, Log: a.errOut, Timeout: 5 * time.Minute}
		if f.noBrowser {
			opts.Paste = a.reader()
		}
		res, err = a.prov().Login(ctx, opts)
		if err != nil {
			return err
		}
		// Lock only after the browser step, so a slow consent never blocks other
		// commands that need to refresh this profile.
		unlock, err := auth.LockProfile(ctx, cfgPath, name)
		if err != nil {
			return err
		}
		defer unlock()
		blob := auth.Blob{RefreshToken: res.Token.RefreshToken, AccessToken: res.Token.AccessToken, Expiry: res.Token.Expiry, Generation: randomGeneration()}
		if cl.Source == config.ClientCustom {
			blob.ClientSecret = cl.Secret
		}
		prefer := secrets.Keychain
		if f.insecureStorage {
			prefer = secrets.File
		}
		saved, err = auth.SaveBlob(ctx, store, prefer, name, blob)
		if err != nil && prefer == secrets.Keychain {
			a.warn("could not save to the OS keychain: %v", err)
			if a.isTerminal() && !a.noInput && a.confirm(fmt.Sprintf("Store this login in a plaintext file readable only by you (%s) instead?", store.FilePath)) == nil {
				saved, err = auth.SaveBlob(ctx, store, secrets.File, name, blob)
			} else {
				return auth.DescribeStoreError(err, name)
			}
		}
		if err != nil {
			return err
		}
		cfg, _, err := a.loadConfig()
		if err != nil {
			return err
		}
		old, existed := cfg.Profiles[name]
		if existed && old.Auth == config.AuthOAuth && storeOf(old) != saved.Backend {
			_ = store.Delete(ctx, storeOf(old), name)
		}
		p = config.Profile{Auth: config.AuthOAuth, Account: res.Email, AccountID: res.Subject, Client: cl.Source, ClientID: cl.ID,
			Scopes: res.Scopes, TokenStore: string(saved.Backend), Generation: blob.Generation}
		if existed && old.AccountID != "" && old.AccountID == res.Subject {
			p.Site = old.Site
		}
		if err := a.saveProfile(ctx, name, p); err != nil {
			return err
		}
		return nil
	}()
	if err != nil {
		return err
	}
	who := res.Email
	if who == "" {
		who = "your Google account"
	}
	a.info("Logged in as %s (profile %q).", who, name)
	if saved.Backend == secrets.File {
		a.warn("the login is stored in plaintext at %s (mode 0600). Google's OAuth policy asks for encrypted storage; prefer the OS keychain where available.", store.FilePath)
	}
	s := &auth.Session{Provider: a.prov(), Store: store, ConfigPath: cfgPath, ProfileName: name, Profile: p}
	chosen, count := p.Site, -1
	if !f.noVerify {
		chosen, count = a.chooseDefaultSite(ctx, name, s, p.Site)
	}
	return a.print(map[string]any{"profile": name, "auth": p.Auth, "account": p.Account, "client": p.Client,
		"scopes": shortScopes(p.Scopes), "token_store": p.TokenStore, "site": nullIfEmpty(chosen), "properties": countOrNil(count)})
}

func (a *app) loginServiceAccount(ctx context.Context, name string, f loginFlags, scope string) error {
	keyFile, err := filepath.Abs(f.serviceAccount)
	if err != nil {
		return err
	}
	ts, email, err := a.prov().ServiceAccount(ctx, keyFile, f.subject, []string{scope})
	if err != nil {
		return err
	}
	p := config.Profile{Auth: config.AuthServiceAccount, Account: email, KeyFile: keyFile, Subject: f.subject, Scopes: []string{scope}, Generation: randomGeneration()}
	if err := a.replaceProfile(ctx, name, p); err != nil {
		return err
	}
	a.info("Saved service account %s in profile %q (the key file stays at %s).", email, name, keyFile)
	a.info("Add %s as a user on each property: Search Console > Settings > Users and permissions.", email)
	chosen, count := "", -1
	if !f.noVerify {
		chosen, count = a.chooseDefaultSite(ctx, name, ts, "")
	}
	return a.print(map[string]any{"profile": name, "auth": p.Auth, "account": email, "key_file": keyFile, "subject": nullIfEmpty(f.subject),
		"site": nullIfEmpty(chosen), "properties": countOrNil(count)})
}

func (a *app) loginADC(ctx context.Context, name string, f loginFlags, scope string) error {
	prov := a.prov()
	var ts client.TokenSource
	if f.impersonate != "" {
		base, err := prov.ADC(ctx, auth.BaseForImpersonation())
		if err != nil {
			return err
		}
		ts = prov.Impersonate(ctx, base.TS, f.impersonate, []string{scope})
	} else {
		adc, err := prov.ADC(ctx, []string{scope})
		if err != nil {
			return err
		}
		ts = adc
	}
	p := config.Profile{Auth: config.AuthADC, Impersonate: f.impersonate, Account: f.impersonate, Scopes: []string{scope}, Generation: randomGeneration()}
	if err := a.replaceProfile(ctx, name, p); err != nil {
		return err
	}
	a.info("Profile %q uses Application Default Credentials; nothing secret was stored.", name)
	chosen, count := "", -1
	if !f.noVerify {
		chosen, count = a.chooseDefaultSite(ctx, name, ts, "")
	}
	return a.print(map[string]any{"profile": name, "auth": p.Auth, "impersonate": nullIfEmpty(f.impersonate), "site": nullIfEmpty(chosen), "properties": countOrNil(count)})
}

// replaceProfile saves a profile that stores no secret (service account, ADC)
// under the profile lock, deleting any OAuth credentials the profile held before
// so they are not left orphaned in the keychain or secrets file.
func (a *app) replaceProfile(ctx context.Context, name string, p config.Profile) error {
	path, err := config.Path()
	if err != nil {
		return err
	}
	unlock, err := auth.LockProfile(ctx, path, name)
	if err != nil {
		return err
	}
	defer unlock()
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	// Save the new profile first: if that fails, the old login stays intact. Only
	// then delete the replaced OAuth credential.
	if err := a.saveProfile(ctx, name, p); err != nil {
		return err
	}
	if old, ok := cfg.Profiles[name]; ok && old.Auth == config.AuthOAuth {
		store, err := a.store()
		if err != nil {
			return err
		}
		if err := store.Delete(ctx, storeOf(old), name); err != nil {
			a.warn("the profile was replaced, but its previous OAuth login could not be deleted: %v", err)
		}
	}
	return nil
}

func (a *app) saveProfile(ctx context.Context, name string, p config.Profile) error {
	path, err := config.Path()
	if err != nil {
		return err
	}
	return config.Update(ctx, path, func(c *config.Config) error {
		c.Profiles[name] = p
		c.CurrentProfile = name
		return nil
	})
}

// chooseDefaultSite lists properties once after login and stores a default
// site: the existing one if still accessible, the only property, a picked
// property (interactive), or the first domain property.
func (a *app) chooseDefaultSite(ctx context.Context, name string, ts client.TokenSource, existing string) (string, int) {
	sites, err := a.newClient(ts).ListSites(ctx)
	if err != nil {
		a.warn("logged in, but listing properties failed: %v", err)
		return existing, -1
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].SiteURL < sites[j].SiteURL })
	chosen := ""
	for _, s := range sites {
		if s.SiteURL == existing {
			chosen = existing
		}
	}
	switch {
	case chosen != "":
	case len(sites) == 0:
		a.info("No properties yet. Add one in Search Console, or run gsc sites add.")
		return "", 0
	case len(sites) == 1:
		chosen = sites[0].SiteURL
	case a.isTerminal() && !a.noInput:
		chosen = a.pickSite(sites)
	default:
		chosen = site.PreferredDefault(sites)
	}
	path, err := config.Path()
	if err == nil {
		err = config.Update(ctx, path, func(c *config.Config) error {
			p, ok := c.Profiles[name]
			if !ok {
				return nil
			}
			p.Site = chosen
			c.Profiles[name] = p
			return nil
		})
	}
	if err != nil {
		a.warn("could not save the default site: %v", err)
	}
	a.info("Found %d %s. Default site set to %s (change with: gsc sites use SITE).", len(sites), plural(len(sites), "property", "properties"), chosen)
	return chosen, len(sites)
}

func (a *app) pickSite(sites []client.SiteEntry) string {
	fmt.Fprintln(a.errOut, "Choose the default site for this profile:")
	for i, s := range sites {
		fmt.Fprintf(a.errOut, "  %d) %s (%s)\n", i+1, s.SiteURL, s.PermissionLevel)
	}
	def := site.PreferredDefault(sites)
	fmt.Fprintf(a.errOut, "Number [Enter for %s]: ", def)
	line, _ := a.reader().ReadString('\n')
	if n, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && n >= 1 && n <= len(sites) {
		return sites[n-1].SiteURL
	}
	return def
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func countOrNil(n int) any {
	if n < 0 {
		return nil
	}
	return n
}

func storeOf(p config.Profile) secrets.Backend {
	if p.TokenStore == config.StoreFile {
		return secrets.File
	}
	return secrets.Keychain
}

func shortScopes(scopes []string) []string {
	out := make([]string, 0, len(scopes))
	for _, s := range scopes {
		out = append(out, strings.TrimPrefix(s, "https://www.googleapis.com/auth/"))
	}
	return out
}

func (a *app) status() *cobra.Command {
	var verify bool
	c := &cobra.Command{Use: "status", Short: "Show which credential is in use, without revealing it", Args: cobra.NoArgs,
		Long: "Reads local configuration only. --verify additionally lists properties with the credential (one API call).",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := a.loadConfig()
			if err != nil {
				return err
			}
			out := map[string]any{"config_file": path}
			switch {
			case a.accessToken != "":
				out["credential_source"] = "flag:--access-token"
			case os.Getenv("GSC_ACCESS_TOKEN") != "":
				out["credential_source"] = "env:GSC_ACCESS_TOKEN"
			case a.credentials != "":
				out["credential_source"] = "flag:--credentials"
			case os.Getenv("GSC_CREDENTIALS") != "":
				out["credential_source"] = "env:GSC_CREDENTIALS"
			}
			name := cfg.SelectProfile(a.profile)
			p, ok := cfg.Profiles[name]
			if ok {
				out["profile"] = name
				out["auth"] = p.Auth
				out["account"] = nullIfEmpty(p.Account)
				if p.Impersonate != "" {
					out["impersonate"] = p.Impersonate
				}
				out["scopes"] = shortScopes(p.Scopes)
				out["write_access"] = len(p.Scopes) == 0 || auth.HasWriteScope(p.Scopes)
				out["site"] = nullIfEmpty(p.Site)
				if p.Auth == config.AuthOAuth {
					out["client"] = p.Client
					out["token_store"] = p.TokenStore
				}
				if _, set := out["credential_source"]; !set {
					out["credential_source"] = "profile:" + name
				}
			} else if name != "" {
				return withKind(kindAuth, fmt.Errorf("profile %q not found; run gsc auth login --profile %s", name, name))
			}
			if _, set := out["credential_source"]; !set {
				return withKind(kindAuth, fmt.Errorf("not logged in (no profile in %s); run gsc auth login (or set GSC_ACCESS_TOKEN or GSC_CREDENTIALS)", path))
			}
			out["read_only_mode"] = a.readOnly
			if verify {
				c, _, err := a.connect(cmd.Context())
				if err != nil {
					return err
				}
				sites, err := c.ListSites(cmd.Context())
				if err != nil {
					return err
				}
				out["verified"] = true
				out["properties"] = len(sites)
			}
			return a.print(out)
		}}
	c.Flags().BoolVar(&verify, "verify", false, "Check the credential by listing properties (one API call)")
	return c
}

func (a *app) listProfiles() *cobra.Command {
	return &cobra.Command{Use: "list", Aliases: []string{"list-profiles"}, Short: "List saved profiles (no secrets)", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _, err := a.loadConfig()
		if err != nil {
			return err
		}
		names := make([]string, 0, len(cfg.Profiles))
		for n := range cfg.Profiles {
			names = append(names, n)
		}
		sort.Strings(names)
		rows := make([]map[string]any, 0, len(names))
		for _, n := range names {
			p := cfg.Profiles[n]
			rows = append(rows, map[string]any{"profile": n, "current": n == cfg.CurrentProfile, "auth": p.Auth, "account": nullIfEmpty(p.Account), "site": nullIfEmpty(p.Site), "token_store": nullIfEmpty(p.TokenStore)})
		}
		return a.print(rows)
	}}
}

func (a *app) useProfile() *cobra.Command {
	return &cobra.Command{Use: "use NAME", Aliases: []string{"use-profile"}, Short: "Set the default profile", Args: cobra.ExactArgs(1),
		Annotations: map[string]string{annWritesLocal: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.Path()
			if err != nil {
				return err
			}
			if err := config.Update(cmd.Context(), path, func(c *config.Config) error {
				if _, ok := c.Profiles[args[0]]; !ok {
					return fmt.Errorf("profile %q not found", args[0])
				}
				c.CurrentProfile = args[0]
				return nil
			}); err != nil {
				return err
			}
			return a.print(map[string]string{"current_profile": args[0]})
		}}
}

func (a *app) logout() *cobra.Command {
	var revoke bool
	c := &cobra.Command{Use: "logout", Short: "Remove a profile and its saved login from this machine", Args: cobra.NoArgs,
		Long: "Deletes the profile and its stored token locally. --revoke also revokes access at Google, which\n" +
			"signs this Google account out of every machine and profile that uses the same OAuth client project;\n" +
			"it asks for confirmation unless --yes is given.",
		Annotations: map[string]string{annWritesLocal: "true", annInteractive: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg, path, err := a.loadConfig()
			if err != nil {
				return err
			}
			name := cfg.SelectProfile(a.profile)
			if name == "" {
				return errors.New("no profile selected and no current profile saved; pass --profile NAME (gsc auth list shows them)")
			}
			p, ok := cfg.Profiles[name]
			if !ok {
				return fmt.Errorf("profile %q not found", name)
			}
			if revoke && p.Auth != config.AuthOAuth {
				return errors.New("--revoke applies only to browser-login profiles")
			}
			unlock, err := auth.LockProfile(ctx, path, name)
			if err != nil {
				return err
			}
			defer unlock()
			// Re-read under the lock: another login may have replaced the profile
			// (auth type or storage backend) while we waited.
			if cfg, err = config.Load(path); err != nil {
				return err
			}
			if p, ok = cfg.Profiles[name]; !ok {
				return fmt.Errorf("profile %q not found", name)
			}
			if revoke && p.Auth != config.AuthOAuth {
				return errors.New("--revoke applies only to browser-login profiles")
			}
			store, err := a.store()
			if err != nil {
				return err
			}
			revoked := false
			if revoke {
				if err := a.confirm("Revoking signs this Google account out of every machine and profile using the same OAuth client. Continue?"); err != nil {
					return err
				}
				blob, err := auth.LoadBlob(store, storeOf(p), name)
				if err != nil {
					return auth.DescribeStoreError(err, name)
				}
				token := blob.RefreshToken
				if token == "" {
					token = blob.AccessToken
				}
				if err := a.prov().Revoke(ctx, token); err != nil {
					return err
				}
				revoked = true
			}
			if p.Auth == config.AuthOAuth {
				if err := store.Delete(ctx, storeOf(p), name); err != nil {
					return err
				}
			}
			if err := config.Update(ctx, path, func(c *config.Config) error {
				delete(c.Profiles, name)
				if c.CurrentProfile == name {
					c.CurrentProfile = ""
				}
				return nil
			}); err != nil {
				return err
			}
			return a.print(map[string]any{"profile": name, "removed": true, "revoked": revoked})
		}}
	c.Flags().BoolVar(&revoke, "revoke", false, "Also revoke access at Google (signs out every machine using the same OAuth client)")
	return c
}

func (a *app) tokenCmd() *cobra.Command {
	return &cobra.Command{Use: "token", Short: "Print a fresh access token for debugging (keep it secret)", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		c, err := a.resolveCreds(cmd.Context())
		if err != nil {
			return err
		}
		tok, err := c.ts.AccessToken(cmd.Context())
		if err != nil {
			return err
		}
		if a.format == "table" {
			_, err := fmt.Fprintln(a.out, tok)
			return err
		}
		return a.print(map[string]any{"access_token": tok, "source": c.source})
	}}
}

func (a *app) configCmd() *cobra.Command {
	c := &cobra.Command{Use: "config", Short: "Inspect configuration and select profiles"}
	list := a.listProfiles()
	list.Use, list.Aliases = "list-profiles", nil
	use := a.useProfile()
	use.Use, use.Aliases = "use-profile NAME", nil
	show := a.status()
	show.Use, show.Aliases = "show", []string{"view"}
	c.AddCommand(list, use, show)
	return c
}

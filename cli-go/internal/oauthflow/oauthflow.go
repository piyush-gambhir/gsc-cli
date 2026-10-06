// Package oauthflow runs the browser half of an OAuth authorization-code
// login for a command-line tool: a one-shot loopback listener on 127.0.0.1, or
// a paste flow for machines without a local browser. It returns the
// authorization code; token exchange (and PKCE) stays with the caller because
// it is provider-specific.
package oauthflow

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"
)

type Options struct {
	// AuthURL builds the provider consent URL for this redirect URI and state.
	AuthURL func(redirectURI, state string) string
	// Port is the loopback port; 0 lets the OS choose (only for providers that
	// accept any loopback port, such as Google Desktop clients).
	Port int
	// Path is the callback path, default "/".
	Path string
	// Timeout bounds the wait for the browser, default 5 minutes.
	Timeout time.Duration
	// OpenBrowser opens the consent URL; nil uses the system browser. It is
	// best-effort: the URL is always printed too.
	OpenBrowser func(string) error
	// Log receives human instructions (stderr in the CLIs, never stdout).
	Log io.Writer
	// Paste, when set, skips the listener: the user approves on any device and
	// pastes the final redirected URL, which is read from this reader.
	Paste io.Reader
	// AllowMissingState accepts a callback without a state parameter, for
	// providers that do not echo it. A present but wrong state is always
	// rejected.
	AllowMissingState bool
}

type Result struct {
	Code        string
	RedirectURI string
}

// Run performs the browser step and returns the authorization code.
func Run(ctx context.Context, o Options) (*Result, error) {
	if o.AuthURL == nil {
		return nil, errors.New("oauthflow: AuthURL is required")
	}
	if o.Path == "" {
		o.Path = "/"
	}
	if o.Timeout == 0 {
		o.Timeout = 5 * time.Minute
	}
	if o.Log == nil {
		o.Log = io.Discard
	}
	state, err := randomState()
	if err != nil {
		return nil, err
	}
	if o.Paste != nil {
		return paste(o, state)
	}
	return listen(ctx, o, state)
}

func listen(ctx context.Context, o Options, state string) (*Result, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", o.Port))
	if err != nil {
		return nil, fmt.Errorf("cannot listen on 127.0.0.1:%d for the login callback (is another login running?): %w", o.Port, err)
	}
	redirect := fmt.Sprintf("http://127.0.0.1:%d%s", ln.Addr().(*net.TCPAddr).Port, o.Path)

	type outcome struct {
		code string
		err  error
	}
	done := make(chan outcome, 1)
	var once sync.Once
	finish := func(r outcome) { once.Do(func() { done <- r }) }

	mux := http.NewServeMux()
	mux.HandleFunc(o.Path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		// Ignore stray requests (favicon, prefetch) that are not a provider callback.
		if r.URL.Path != o.Path || (!q.Has("code") && !q.Has("error")) {
			http.NotFound(w, r)
			return
		}
		code, err := checkCallback(q, state, o.AllowMissingState)
		writePage(w, err)
		finish(outcome{code, err})
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(c)
	}()

	authURL := o.AuthURL(redirect, state)
	fmt.Fprintf(o.Log, "Opening your browser to sign in. If it does not open, visit:\n  %s\n", authURL)
	open := o.OpenBrowser
	if open == nil {
		open = systemBrowser
	}
	_ = open(authURL)

	timer := time.NewTimer(o.Timeout)
	defer timer.Stop()
	select {
	case r := <-done:
		if r.err != nil {
			return nil, r.err
		}
		return &Result{Code: r.code, RedirectURI: redirect}, nil
	case <-timer.C:
		return nil, fmt.Errorf("timed out after %s waiting for the browser sign-in", o.Timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func paste(o Options, state string) (*Result, error) {
	port := o.Port
	if port == 0 {
		port = 8085
	}
	redirect := fmt.Sprintf("http://127.0.0.1:%d%s", port, o.Path)
	fmt.Fprintf(o.Log, "Open this URL in a browser on any device and approve access:\n  %s\n\n", o.AuthURL(redirect, state))
	fmt.Fprintf(o.Log, "The browser will then fail to load a 127.0.0.1 page. That is expected.\nPaste the full URL from its address bar here: ")
	line, err := bufio.NewReader(o.Paste).ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		return nil, fmt.Errorf("no redirect URL was pasted")
	}
	u, err := url.Parse(strings.TrimSpace(line))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("that does not look like the redirected URL; copy the whole address bar")
	}
	code, err := checkCallback(u.Query(), state, o.AllowMissingState)
	if err != nil {
		return nil, err
	}
	return &Result{Code: code, RedirectURI: redirect}, nil
}

func checkCallback(q url.Values, state string, allowMissingState bool) (string, error) {
	// State first: an error callback must belong to this login attempt too.
	got := q.Get("state")
	if got != state && !(got == "" && allowMissingState) {
		return "", errors.New("state mismatch; the callback did not come from this login attempt")
	}
	if e := q.Get("error"); e != "" {
		msg := "authorization was denied (" + printable(e, 64) + ")"
		if d := q.Get("error_description"); d != "" {
			msg += ": " + printable(d, 200)
		}
		return "", errors.New(msg)
	}
	code := q.Get("code")
	if code == "" {
		return "", errors.New("the callback contained no authorization code")
	}
	return code, nil
}

// printable strips control characters (such as terminal escape sequences) from
// provider-supplied text before it reaches the terminal, and caps its length.
func printable(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	if r := []rune(s); len(r) > max {
		s = string(r[:max]) + "..."
	}
	return s
}

func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func systemBrowser(u string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", u).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	default:
		return exec.Command("xdg-open", u).Start()
	}
}

// writePage renders the tab the browser lands on after the provider redirects
// back. The page is self-contained (the CSP forbids scripts and network loads)
// and follows the OS light or dark theme. The browser runs on this machine, so
// runtime.GOOS picks the close-tab shortcut.
func writePage(w http.ResponseWriter, err error) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:")
	h.Set("Referrer-Policy", "no-referrer")
	data := struct {
		OK       bool
		Detail   string
		CloseKey string
	}{OK: err == nil, CloseKey: "Ctrl+W"}
	if runtime.GOOS == "darwin" {
		data.CloseKey = "⌘W"
	}
	if err != nil {
		data.Detail = err.Error()
		w.WriteHeader(http.StatusBadRequest)
	}
	_ = page.Execute(w, data)
}

var page = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{if .OK}}Signed in{{else}}Sign-in failed{{end}} · gsc</title>
<link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 32 32'%3E%3Crect width='32' height='32' rx='7' fill='%23131412'/%3E%3Cpath d='M8.5 10.5 15 16l-6.5 5.5' fill='none' stroke='%237ca2f6' stroke-width='3' stroke-linecap='round' stroke-linejoin='round'/%3E%3Crect x='17.5' y='20' width='7' height='3' rx='1.5' fill='%237ca2f6'/%3E%3C/svg%3E">
<style>
:root{color-scheme:light dark;--page:#f3f4f1;--card:#fff;--line:#e0e1de;--text:#131412;--muted:#4e524a;--faint:#7f827b;--code:#e9eae7;--ok:oklch(0.62 0.15 265);--bad:oklch(0.6 0.19 28)}
@media (prefers-color-scheme:dark){:root{--page:#131412;--card:#1e201b;--line:#30342c;--text:#f3f4f1;--muted:#b6b8b3;--code:#272a24;--ok:oklch(0.72 0.13 265);--bad:oklch(0.7 0.16 28)}}
*{box-sizing:border-box;margin:0}
body{min-height:100vh;display:grid;place-items:center;padding:24px;background:var(--page);color:var(--text);font:15px/1.55 ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif;-webkit-font-smoothing:antialiased}
main{width:100%;max-width:440px}
code,kbd,.err{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,"Liberation Mono",monospace}
.brand{display:flex;align-items:center;gap:8px;margin-bottom:56px;color:var(--muted);font-weight:600;font-size:14px}
.brand svg{width:20px;height:20px}.brand .bg{stroke:var(--line)}
.icon{width:40px;height:40px;border-radius:12px;display:grid;place-items:center;margin-bottom:20px;color:var(--ok);background:color-mix(in oklab,var(--ok) 15%,transparent)}
.icon.bad{color:var(--bad);background:color-mix(in oklab,var(--bad) 14%,transparent)}
.icon svg{width:22px;height:22px}
h1{font-size:30px;line-height:1.15;font-weight:650;letter-spacing:-.025em;margin-bottom:10px}
p{color:var(--muted);font-size:16px}
p code{font-size:.86em;color:var(--text);background:var(--code);padding:2px 6px;border-radius:6px}
.err{margin-top:14px;font-size:13px;color:var(--faint);overflow-wrap:anywhere}
.close{margin-top:40px;padding-top:16px;border-top:1px solid var(--line);display:flex;align-items:center;gap:8px;font-size:13px;color:var(--faint)}
kbd{font-size:12px;padding:1px 6px;border:1px solid var(--line);border-bottom-width:2px;border-radius:6px;background:var(--card);color:var(--muted)}
</style>
<main>
<div class="brand"><svg viewBox="0 0 32 32" aria-hidden="true"><rect class="bg" x=".5" y=".5" width="31" height="31" rx="7" fill="#131412"/><path d="M8.5 10.5 15 16l-6.5 5.5" fill="none" stroke="#7ca2f6" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"/><rect x="17.5" y="20" width="7" height="3" rx="1.5" fill="#7ca2f6"/></svg>gsc</div>
{{if .OK}}<div class="icon"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5.5 12.5 10 17l8.5-9.5" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"/></svg></div>
<h1>You're signed in</h1>
<p>Return to your terminal, where gsc is finishing setup.</p>
{{else}}<div class="icon bad"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M7.5 7.5l9 9m0-9-9 9" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round"/></svg></div>
<h1>Sign-in didn't finish</h1>
<p>Run <code>gsc auth login</code> in your terminal to try again.</p>
<p class="err">{{.Detail}}</p>
{{end}}<div class="close"><kbd>{{.CloseKey}}</kbd> closes this tab</div>
</main>
</html>
`))

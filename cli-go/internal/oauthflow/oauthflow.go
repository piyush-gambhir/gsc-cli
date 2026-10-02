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
	"html"
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
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, page, "Sign-in failed", html.EscapeString(err.Error())+" You can close this tab.")
		} else {
			fmt.Fprintf(w, page, "Signed in", "You can close this tab and return to the terminal.")
		}
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

const page = `<!doctype html><meta charset="utf-8"><title>%[1]s</title><body style="font-family:system-ui;margin:4rem auto;max-width:32rem"><h2>%[1]s</h2><p>%[2]s</p></body>`

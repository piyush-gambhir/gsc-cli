package oauthflow

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// consent mimics a provider: the auth URL carries redirect_uri and state, and
// the fake browser follows it with the given callback parameters.
func consent(redirectURI, state string) string {
	return "https://provider.example/authorize?" + url.Values{"redirect_uri": {redirectURI}, "state": {state}}.Encode()
}

func browser(t *testing.T, params func(state string) url.Values) func(string) error {
	return func(authURL string) error {
		u, _ := url.Parse(authURL)
		q := u.Query()
		go func() {
			// A stray request first: it must not end the login.
			if res, err := http.Get(q.Get("redirect_uri") + "favicon.ico"); err == nil {
				res.Body.Close()
			}
			res, err := http.Get(q.Get("redirect_uri") + "?" + params(q.Get("state")).Encode())
			if err != nil {
				t.Error(err)
				return
			}
			res.Body.Close()
		}()
		return nil
	}
}

func TestLoopbackSuccess(t *testing.T) {
	var log bytes.Buffer
	r, err := Run(context.Background(), Options{AuthURL: consent, Log: &log, OpenBrowser: browser(t, func(s string) url.Values {
		return url.Values{"code": {"abc"}, "state": {s}}
	})})
	if err != nil || r.Code != "abc" || !strings.HasPrefix(r.RedirectURI, "http://127.0.0.1:") {
		t.Fatalf("%+v %v", r, err)
	}
	if !strings.Contains(log.String(), "https://provider.example/authorize") {
		t.Fatalf("URL not printed: %s", log.String())
	}
}

func TestLoopbackRejectsWrongStateAndDenial(t *testing.T) {
	for name, params := range map[string]func(string) url.Values{
		"wrong state":   func(string) url.Values { return url.Values{"code": {"abc"}, "state": {"forged"}} },
		"missing state": func(string) url.Values { return url.Values{"code": {"abc"}} },
		"denied":        func(s string) url.Values { return url.Values{"error": {"access_denied"}, "state": {s}} },
		"forged denial": func(string) url.Values { return url.Values{"error": {"access_denied"}} },
		"no code":       func(s string) url.Values { return url.Values{"error": {""}, "code": {""}, "state": {s}} },
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Run(context.Background(), Options{AuthURL: consent, OpenBrowser: browser(t, params), Timeout: 5 * time.Second})
			if err == nil {
				t.Fatal("login succeeded")
			}
		})
	}
}

func TestAllowMissingState(t *testing.T) {
	r, err := Run(context.Background(), Options{AuthURL: consent, AllowMissingState: true, OpenBrowser: browser(t, func(string) url.Values {
		return url.Values{"code": {"abc"}}
	})})
	if err != nil || r.Code != "abc" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestTimeoutAndCancel(t *testing.T) {
	noop := func(string) error { return nil }
	if _, err := Run(context.Background(), Options{AuthURL: consent, OpenBrowser: noop, Timeout: 50 * time.Millisecond}); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, Options{AuthURL: consent, OpenBrowser: noop}); err == nil {
		t.Fatal("canceled login succeeded")
	}
}

func TestFixedPortInUse(t *testing.T) {
	noop := func(string) error { return nil }
	first := make(chan struct{})
	go func() {
		_, _ = Run(context.Background(), Options{AuthURL: func(r, s string) string { close(first); return consent(r, s) }, Port: 47999, OpenBrowser: noop, Timeout: time.Second})
	}()
	<-first
	if _, err := Run(context.Background(), Options{AuthURL: consent, Port: 47999, OpenBrowser: noop}); err == nil || !strings.Contains(err.Error(), "cannot listen") {
		t.Fatalf("expected port error: %v", err)
	}
}

func TestPasteFlow(t *testing.T) {
	var log bytes.Buffer
	var state string
	o := Options{Log: &log, AuthURL: func(r, s string) string { state = s; return consent(r, s) }}
	// The pasted URL is built after AuthURL runs, so feed it through a pipe-like reader.
	pr := &lazyReader{line: func() string { return "http://127.0.0.1:8085/?code=xyz&state=" + url.QueryEscape(state) + "\n" }}
	o.Paste = pr
	r, err := Run(context.Background(), o)
	if err != nil || r.Code != "xyz" || r.RedirectURI != "http://127.0.0.1:8085/" {
		t.Fatalf("%+v %v", r, err)
	}
	o.Paste = strings.NewReader("not a url\n")
	if _, err := Run(context.Background(), o); err == nil {
		t.Fatal("accepted garbage")
	}
	o.Paste = strings.NewReader("http://127.0.0.1:8085/?code=xyz&state=forged\n")
	if _, err := Run(context.Background(), o); err == nil {
		t.Fatal("accepted forged state")
	}
}

type lazyReader struct {
	line func() string
	r    *strings.Reader
}

func (l *lazyReader) Read(p []byte) (int, error) {
	if l.r == nil {
		l.r = strings.NewReader(l.line())
	}
	return l.r.Read(p)
}

func TestErrorCallbacksNeedStateAndAreSanitized(t *testing.T) {
	if _, err := checkCallback(url.Values{"error": {"access_denied"}}, "s1", false); err == nil || !strings.Contains(err.Error(), "state mismatch") {
		t.Fatalf("stateless error callback: %v", err)
	}
	_, err := checkCallback(url.Values{"error": {"access_denied"}, "error_description": {"bad\x1b[2Jthing\r\n" + strings.Repeat("x", 500)}, "state": {"s1"}}, "s1", false)
	if err == nil || strings.ContainsAny(err.Error(), "\x1b\r\n") || len(err.Error()) > 320 || !strings.Contains(err.Error(), "bad[2Jthing") {
		t.Fatalf("unsanitized error: %q", err)
	}
}

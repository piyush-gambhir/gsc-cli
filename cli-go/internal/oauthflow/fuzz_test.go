package oauthflow

import (
	"net/url"
	"strings"
	"testing"
	"unicode"
)

// FuzzCheckCallback checks the loopback callback and pasted-URL parser: a code
// is returned only for the expected state, and provider-supplied error text
// reaches the terminal without control characters and bounded in length.
// `go test -run=^$ -fuzz=FuzzCheckCallback ./internal/oauthflow` explores further.
func FuzzCheckCallback(f *testing.F) {
	f.Add("code=c1&state=s1", "s1", false)
	f.Add("code=c1", "s1", true)
	f.Add("error=access_denied&state=s1", "s1", false)
	f.Add("error=%1b%5b31m&error_description=%07bell&state=s1", "s1", false)
	f.Add("state=s1&state=s2&code=c", "s2", false)
	f.Fuzz(func(t *testing.T, rawQuery, state string, allowMissing bool) {
		q, err := url.ParseQuery(rawQuery)
		if err != nil {
			return
		}
		code, err := checkCallback(q, state, allowMissing)
		if err != nil {
			if strings.ContainsFunc(err.Error(), unicode.IsControl) || len(err.Error()) > 2048 {
				t.Fatalf("unsafe error text %q", err)
			}
			return
		}
		got := q.Get("state")
		if code == "" || code != q.Get("code") || !(got == state || allowMissing && got == "") {
			t.Fatalf("accepted %q for state %q (allowMissing=%v): code %q", rawQuery, state, allowMissing, code)
		}
	})
}

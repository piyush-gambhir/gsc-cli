package client

import (
	"strings"
	"testing"
)

func TestSafeURLMasksCredentialParameters(t *testing.T) {
	got := SafeURL("https://searchconsole.googleapis.com/webmasters/v3/sites?access_token=s1&Key=s2&client_secret=s3&code=s4&fields=siteEntry")
	for _, secret := range []string{"s1", "s2", "s3", "s4"} {
		if strings.Contains(got, "="+secret) {
			t.Fatalf("%s not masked: %s", secret, got)
		}
	}
	if !strings.Contains(got, "fields=siteEntry") {
		t.Fatalf("ordinary parameter lost: %s", got)
	}
}

func TestSafeURLMasksUnparseableQueries(t *testing.T) {
	if got := SafeURL("https://x.example/p?access_token=secret;extra"); strings.Contains(got, "secret") {
		t.Fatalf("unparseable query leaked: %s", got)
	}
}

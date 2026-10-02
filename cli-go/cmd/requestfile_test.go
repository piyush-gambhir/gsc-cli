package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequestFileRowLimitStandsUnlessLimitIsGiven(t *testing.T) {
	isolate(t)
	t.Setenv("GSC_ACCESS_TOKEN", "env-token")
	file := filepath.Join(t.TempDir(), "req.json")
	if err := os.WriteFile(file, []byte(`{"startDate":"2026-09-01","endDate":"2026-09-30","dimensions":["query"],"rowLimit":5}`), 0600); err != nil {
		t.Fatal(err)
	}
	f := newFake(t)
	r := cli(t, f, now, "", "query", "-s", "sc-domain:example.com", "--request-file", file, "--print-request", "-o", "json")
	if r.code != 0 || !strings.Contains(r.out, `"rowLimit": 5`) {
		t.Fatalf("file rowLimit overridden by the default --limit: %s %s", r.out, r.errOut)
	}
	r = cli(t, f, now, "", "query", "-s", "sc-domain:example.com", "--request-file", file, "--limit", "7", "--print-request", "-o", "json")
	if r.code != 0 || !strings.Contains(r.out, `"rowLimit": 7`) {
		t.Fatalf("explicit --limit must override the file: %s %s", r.out, r.errOut)
	}
}

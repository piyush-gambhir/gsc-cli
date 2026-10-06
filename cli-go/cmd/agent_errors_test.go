package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
)

// A typo under a command group is a usage error with a suggestion, not help
// on stdout with exit 0; the bare group still prints help.
func TestUnknownSubcommandFails(t *testing.T) {
	isolate(t)
	f := newFake(t)
	r := cli(t, f, now, "", "sites", "lsit", "-o", "json")
	var e map[string]any
	if r.code != 1 || r.out != "" || json.Unmarshal([]byte(r.errOut), &e) != nil || !strings.Contains(e["error"].(string), `unknown command "lsit"`) || !strings.Contains(e["error"].(string), "list") {
		t.Fatalf("nested typo: code=%d out=%q err=%q", r.code, r.out, r.errOut)
	}
	if r := cli(t, f, now, "", "sites"); r.code != 0 || !strings.Contains(r.out, "Available Commands") {
		t.Fatalf("bare group: code=%d out=%q", r.code, r.out)
	}
}

// Errors raised before Cobra parses -o still come out structured.
func TestEarlyErrorsHonorOutputFlag(t *testing.T) {
	isolate(t)
	f := newFake(t)
	for _, args := range [][]string{{"-o", "json", "qurey"}, {"qurey", "--output=json"}, {"query", "--limti", "5", "-o", "json"}} {
		r := cli(t, f, now, "", args...)
		var e map[string]any
		if r.code != 1 || json.Unmarshal([]byte(r.errOut), &e) != nil || e["error"] == nil {
			t.Fatalf("%v: code=%d err=%q", args, r.code, r.errOut)
		}
	}
	if r := cli(t, f, now, "", "qurey", "-o", "yaml"); r.code != 1 || !strings.HasPrefix(r.errOut, "error:") {
		t.Fatalf("yaml: %q", r.errOut)
	}
	// A parsed -o wins over a later flag value that merely looks like one.
	t.Setenv("GSC_ACCESS_TOKEN", "env-token")
	r := cli(t, f, now, "", "query", "-s", "sc-domain:example.com", "-o", "json", "--filter", "-oyaml")
	var e map[string]any
	if r.code != 1 || json.Unmarshal([]byte(r.errOut), &e) != nil {
		t.Fatalf("flag value mistaken for -o: code=%d err=%q", r.code, r.errOut)
	}
}

// Tables drop the envelope, so preliminary rows get a stderr note; JSON labels them itself.
func TestPreliminaryDataNoteForTables(t *testing.T) {
	isolate(t)
	t.Setenv("GSC_ACCESS_TOKEN", "env-token")
	f := newFake(t)
	f.query = func(req client.QueryRequest) client.QueryResponse {
		return client.QueryResponse{Rows: []client.Row{{Keys: []string{"2026-10-02"}, Clicks: 1, Impressions: 10}}, Metadata: &client.Metadata{FirstIncompleteDate: "2026-10-02"}}
	}
	args := []string{"query", "-s", "sc-domain:example.com", "-d", "date", "--data-state", "all", "--start", "2026-09-30", "--end", "2026-10-02"}
	if r := cli(t, f, now, "", args...); r.code != 0 || !strings.Contains(r.errOut, "data from 2026-10-02 on is preliminary") {
		t.Fatalf("table: code=%d err=%q", r.code, r.errOut)
	}
	if r := cli(t, f, now, "", append(args, "-o", "json")...); r.code != 0 || strings.Contains(r.errOut, "preliminary") {
		t.Fatalf("json: code=%d err=%q", r.code, r.errOut)
	}
}

// --help before a flag with a value still shows help (Cobra registers --help
// lazily, so it used to swallow -o and fail on "json" as a command).
func TestHelpBeforeValueFlag(t *testing.T) {
	isolate(t)
	f := newFake(t)
	if r := cli(t, f, now, "", "--help", "-o", "json"); r.code != 0 || !strings.Contains(r.out, "Available Commands") {
		t.Fatalf("code=%d out=%q err=%q", r.code, r.out, r.errOut)
	}
}

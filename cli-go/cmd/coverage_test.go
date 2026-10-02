package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/coverage"
	"github.com/spf13/cobra"
)

type discovery struct {
	Revision  string              `json:"revision"`
	Resources map[string]resource `json:"resources"`
}

type resource struct {
	Methods map[string]struct {
		ID         string `json:"id"`
		HTTPMethod string `json:"httpMethod"`
		Path       string `json:"path"`
		FlatPath   string `json:"flatPath"`
	} `json:"methods"`
	Resources map[string]resource `json:"resources"`
}

func methods(res map[string]resource, out map[string][2]string) {
	for _, r := range res {
		for _, m := range r.Methods {
			p := m.FlatPath
			if p == "" {
				p = m.Path
			}
			out[m.ID] = [2]string{m.HTTPMethod, p}
		}
		methods(r.Resources, out)
	}
}

func runnable(root *cobra.Command) map[string]bool {
	out := map[string]bool{}
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Runnable() {
			out[c.CommandPath()] = true
		}
		for _, child := range c.Commands() {
			walk(child)
		}
	}
	walk(root)
	return out
}

// TestAPICoverage fails if any method in the vendored discovery snapshots is
// neither implemented by a runnable command nor skipped with a reason, or if
// docs/api-coverage.md does not list it.
func TestAPICoverage(t *testing.T) {
	snapshot := map[string][2]string{}
	for _, s := range coverage.Snapshots {
		b, err := os.ReadFile(filepath.Join("..", "internal", "coverage", "testdata", s.File))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != s.SHA256 {
			t.Fatalf("%s sha256 changed; update coverage.Snapshots and docs/compatibility.md", s.File)
		}
		var d discovery
		if err := json.Unmarshal(b, &d); err != nil {
			t.Fatal(err)
		}
		if d.Revision != s.Revision {
			t.Fatalf("%s revision %s, table says %s", s.File, d.Revision, s.Revision)
		}
		methods(d.Resources, snapshot)
	}
	commands := runnable(NewRoot(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}))
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "api-coverage.md"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range coverage.Entries {
		seen[e.ID] = true
		got, ok := snapshot[e.ID]
		if !ok {
			t.Errorf("%s is in the coverage table but not in the snapshot", e.ID)
			continue
		}
		if got[0] != e.HTTP || got[1] != e.Path {
			t.Errorf("%s: snapshot says %s %s, table says %s %s", e.ID, got[0], got[1], e.HTTP, e.Path)
		}
		if !strings.Contains(string(doc), "`"+e.ID+"`") {
			t.Errorf("docs/api-coverage.md does not list %s", e.ID)
		}
		switch e.Status {
		case coverage.Implemented:
			if len(e.Commands) == 0 {
				t.Errorf("%s is implemented but maps to no command", e.ID)
			}
			for _, c := range e.Commands {
				if !commands[c] {
					t.Errorf("%s maps to %q, which is not a runnable command", e.ID, c)
				}
				if !strings.Contains(string(doc), "`"+c+"`") {
					t.Errorf("docs/api-coverage.md does not mention %q", c)
				}
			}
		case coverage.Skipped, coverage.Retired:
			if e.Reason == "" || len(e.Commands) != 0 {
				t.Errorf("%s must have a reason and no commands", e.ID)
			}
		default:
			t.Errorf("%s has unknown status %q", e.ID, e.Status)
		}
	}
	for id := range snapshot {
		if !seen[id] {
			t.Errorf("snapshot method %s is neither implemented nor skipped", id)
		}
	}
}

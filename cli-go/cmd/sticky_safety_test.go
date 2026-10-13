package cmd

import (
	"bytes"
	"strings"
	"testing"
)

// Safety settings from the environment cannot be switched off by a flag: an agent
// runner sets GSC_READ_ONLY and must be able to rely on it.
func TestEnvironmentReadOnlyIsSticky(t *testing.T) {
	t.Setenv("GSC_READ_ONLY", "true")
	t.Setenv("GSC_NO_UPDATE_NOTIFIER", "1")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, args := range [][]string{
		{"sites", "add", "https://example.com/", "--read-only=false", "-o", "json"},
		{"auth", "token", "--read-only=false", "-o", "json"},
		{"export", "--last", "7d", "--out", t.TempDir(), "--read-only=false", "-o", "json"},
	} {
		var out, errOut bytes.Buffer
		root := NewRoot(strings.NewReader(""), &out, &errOut)
		root.SetArgs(args)
		err := root.Execute()
		if err == nil || !strings.Contains(err.Error(), "blocked by --read-only") {
			t.Errorf("%v: want a read-only refusal, got %v", args, err)
		}
	}
}

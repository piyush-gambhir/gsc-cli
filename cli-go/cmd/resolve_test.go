package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// commands resolve uses the real parser, so a flag placed between command words
// cannot hide the command or its effect from a runner.
func TestCommandsResolve(t *testing.T) {
	cases := []struct {
		args    []string
		command string
		effect  string
		secret  bool
	}{
		{[]string{"top", "queries", "-s", "sc-domain:x.com", "--last", "7d"}, "gsc top queries", "read", false},
		{[]string{"auth", "-o", "json", "token"}, "gsc auth token", "read", true},
		{[]string{"sites", "add", "https://x.com/"}, "gsc sites add", "remote_write", false},
		{[]string{"export", "--out", "d", "--last", "7d"}, "gsc export", "local_write", false},
	}
	for _, tc := range cases {
		var out, errOut bytes.Buffer
		root := NewRoot(strings.NewReader(""), &out, &errOut)
		root.SetArgs(append([]string{"commands", "resolve", "--"}, tc.args...))
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		var got resolution
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatalf("%v: %v in %q", tc.args, err, out.String())
		}
		if got.Command != tc.command || got.Effect != tc.effect || got.Secret != tc.secret {
			t.Errorf("%v: got %+v", tc.args, got)
		}
	}
	root := NewRoot(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	root.SetArgs([]string{"commands", "resolve", "--", "nonsense"})
	if err := root.Execute(); err == nil {
		t.Fatal("an unknown command resolved")
	}
}

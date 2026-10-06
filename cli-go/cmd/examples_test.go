package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Every runnable leaf command shows at least one example in its help, and
// every example names the command it belongs to.
func TestEveryCommandHasExamples(t *testing.T) {
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.Runnable() && !sub.HasSubCommands() {
				if sub.Example == "" {
					t.Errorf("%s has no Example", sub.CommandPath())
				} else if !strings.Contains(sub.Example, sub.CommandPath()) {
					t.Errorf("%s examples do not use the command: %q", sub.CommandPath(), sub.Example)
				}
			}
			walk(sub)
		}
	}
	walk(NewRoot(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}))
	for path := range examples {
		if !strings.HasPrefix(path, "gsc ") {
			t.Errorf("bad example key %q", path)
		}
	}
}

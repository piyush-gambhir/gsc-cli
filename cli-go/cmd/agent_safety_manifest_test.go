package cmd

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestAgentSafetyCommandManifest makes every runnable command part of a
// reviewed safety contract. Adding, removing, or reclassifying a command
// requires an intentional manifest-hash update.
func TestAgentSafetyCommandManifest(t *testing.T) {
	writeVerbs := map[string]bool{"add": true, "remove": true, "delete": true, "submit": true, "login": true, "logout": true, "use": true, "use-profile": true, "update": true}
	var entries []string
	var walk func(*cobra.Command)
	walk = func(parent *cobra.Command) {
		for _, c := range parent.Commands() {
			if c.Runnable() {
				ann := c.Annotations
				mutates, local, interactive := ann[annMutates], ann[annWritesLocal], ann[annInteractive]
				leaf := strings.Fields(c.Use)[0]
				if writeVerbs[leaf] && mutates == "" && local == "" {
					t.Errorf("%q looks like a write but has no mutates or writes-local annotation", c.CommandPath())
				}
				entries = append(entries, fmt.Sprintf("%s|mutates=%s|writes-local=%s|interactive=%s", c.CommandPath(), mutates, local, interactive))
			}
			walk(c)
		}
	}
	walk(NewRoot(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}))
	sort.Strings(entries)
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(entries, "\n"))))
	const expectedDigest = "2992c61484b6f4904fe9c5fc5349f53d841f000d600dd3b09fbb64997998a6e1"
	if digest != expectedDigest {
		t.Fatalf("agent-safety command manifest changed: got %s\n%s\nreview the annotations above, then update expectedDigest", digest, strings.Join(entries, "\n"))
	}
}

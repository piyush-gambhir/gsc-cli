package cmd

import (
	"encoding/json"
	"testing"
)

// gsc commands describes every runnable command offline, with effects that match
// the safety annotations, and never reads config or calls the network.
func TestCommandsDescribesTree(t *testing.T) {
	isolate(t)
	f := newFake(t)
	r := cli(t, f, now, "", "commands", "-o", "json")
	var list commandList
	if r.code != 0 || json.Unmarshal([]byte(r.out), &list) != nil || f.total() != 0 {
		t.Fatalf("code=%d calls=%d err=%q", r.code, f.total(), r.errOut)
	}
	byName := map[string]commandInfo{}
	for _, c := range list.Commands {
		byName[c.Command] = c
	}
	for name, want := range map[string]string{"gsc sites remove": "remote_write", "gsc auth login": "local_write", "gsc query": "read", "gsc api": "remote_write"} {
		if byName[name].Effect != want {
			t.Errorf("%s effect %q, want %q", name, byName[name].Effect, want)
		}
	}
	if !byName["gsc api"].Conditional || len(byName["gsc query"].APIMethods) != 1 || len(byName["gsc sites"].Command) != 0 {
		t.Errorf("api conditional, query methods, or a group leaked: %+v %+v", byName["gsc api"], byName["gsc query"])
	}
	if len(list.GlobalFlags) == 0 || len(byName["gsc query"].Flags) == 0 || len(byName["gsc query"].Examples) == 0 {
		t.Errorf("missing flags or examples")
	}
}

// gsc api methods lists the pinned registry, with read effects for the read-only POSTs.
func TestAPIMethods(t *testing.T) {
	isolate(t)
	f := newFake(t)
	r := cli(t, f, now, "", "api", "methods", "-o", "json")
	var rows []map[string]any
	if r.code != 0 || json.Unmarshal([]byte(r.out), &rows) != nil || f.total() != 0 || len(rows) != 13 {
		t.Fatalf("code=%d rows=%d err=%q", r.code, len(rows), r.errOut)
	}
	effects := map[string]any{}
	for _, m := range rows {
		effects[m["method"].(string)] = m["effect"]
	}
	if effects["webmasters.searchanalytics.query"] != "read" || effects["searchconsole.urlInspection.index.inspect"] != "read" || effects["webmasters.sites.delete"] != "write" {
		t.Errorf("effects: %v", effects)
	}
}

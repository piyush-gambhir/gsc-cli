package site

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
)

func lister(sites ...string) (func(context.Context) ([]client.SiteEntry, error), *int) {
	calls := 0
	return func(context.Context) ([]client.SiteEntry, error) {
		calls++
		out := make([]client.SiteEntry, len(sites))
		for i, s := range sites {
			out[i] = client.SiteEntry{SiteURL: s, PermissionLevel: "siteOwner"}
		}
		return out, nil
	}, &calls
}

func TestResolve(t *testing.T) {
	ctx := context.Background()
	list, calls := lister("https://www.example.com/", "sc-domain:example.com", "https://blog.other.com/", "http://other.com/", "https://other.com/")
	for in, want := range map[string]string{
		"sc-domain:anything.com":   "sc-domain:anything.com",
		"https://www.example.com/": "https://www.example.com/",
		"example.com":              "sc-domain:example.com",
		"www.example.com":          "sc-domain:example.com",
		"blog.other.com":           "https://blog.other.com/",
	} {
		got, err := Resolve(ctx, in, list)
		if err != nil || got != want {
			t.Fatalf("%s: %q %v", in, got, err)
		}
	}
	before := *calls
	if _, err := Resolve(ctx, "sc-domain:x.com", list); err != nil || *calls != before {
		t.Fatal("exact identifier triggered a lookup")
	}
	if _, err := Resolve(ctx, "other.com", list); err == nil || !strings.Contains(err.Error(), "several") {
		t.Fatalf("ambiguity: %v", err)
	}
	for _, bad := range []string{"", "nothing.com", "example.com/path"} {
		if _, err := Resolve(ctx, bad, list); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	failing := func(context.Context) ([]client.SiteEntry, error) { return nil, errors.New("api down") }
	if _, err := Resolve(ctx, "example.com", failing); err == nil {
		t.Fatal("swallowed list error")
	}
}

func TestPreferredDefault(t *testing.T) {
	sites := []client.SiteEntry{{SiteURL: "https://b.com/", PermissionLevel: "siteOwner"}, {SiteURL: "sc-domain:z.com", PermissionLevel: "siteUnverifiedUser"}, {SiteURL: "sc-domain:a.com", PermissionLevel: "siteFullUser"}}
	if got := PreferredDefault(sites); got != "sc-domain:a.com" {
		t.Fatal(got)
	}
	if PreferredDefault(nil) != "" {
		t.Fatal("non-empty default for no sites")
	}
}

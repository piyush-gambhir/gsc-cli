package cmd

import (
	"testing"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
)

// sites add reports the permission Google granted instead of assuming the new
// property is unverified: a URL-prefix under an owned domain is owned at once.
func TestSitesAddReportsGrantedPermission(t *testing.T) {
	isolate(t)
	t.Setenv("GSC_ACCESS_TOKEN", "env-token")
	f := newFake(t)
	f.sites = append(f.sites,
		client.SiteEntry{SiteURL: "https://www.example.com/", PermissionLevel: "siteOwner"},
		client.SiteEntry{SiteURL: "https://other.example/", PermissionLevel: "siteUnverifiedUser"})
	for site, verified := range map[string]bool{"https://www.example.com/": true, "https://other.example/": false} {
		r := cli(t, f, now, "", "sites", "add", site, "-o", "json")
		if r.code != 0 {
			t.Fatalf("add %s: %s", site, r.errOut)
		}
		if m := decode(t, r.out); m["added"] != true || m["verified"] != verified {
			t.Fatalf("add %s: %s", site, r.out)
		}
	}
}

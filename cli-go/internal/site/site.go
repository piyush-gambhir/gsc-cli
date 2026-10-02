// Package site resolves user input to a Search Console property identifier.
package site

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
)

// Exact reports whether s is already a property identifier that must be used
// verbatim: a domain property or a URL-prefix property.
func Exact(s string) bool {
	return strings.HasPrefix(s, "sc-domain:") || strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// Resolve maps a bare host such as example.com to the caller's property:
// the domain property for the host (or its parent when the host starts with
// www.) wins; otherwise exactly one URL-prefix property on that host.
// Exact identifiers are returned unchanged without any request.
func Resolve(ctx context.Context, input string, list func(context.Context) ([]client.SiteEntry, error)) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("no site given; pass --site, set GSC_SITE, or run gsc sites use SITE")
	}
	if Exact(input) {
		return input, nil
	}
	host := strings.ToLower(strings.TrimSuffix(input, "/"))
	if strings.ContainsAny(host, "/?#") {
		return "", fmt.Errorf("%q is not a host or property; use sc-domain:example.com or a full URL-prefix such as https://www.example.com/", input)
	}
	sites, err := list(ctx)
	if err != nil {
		return "", err
	}
	domains := []string{"sc-domain:" + host}
	if strings.HasPrefix(host, "www.") {
		domains = append(domains, "sc-domain:"+strings.TrimPrefix(host, "www."))
	}
	for _, d := range domains {
		for _, s := range sites {
			if strings.EqualFold(s.SiteURL, d) {
				return s.SiteURL, nil
			}
		}
	}
	var matches []string
	for _, s := range sites {
		u, err := url.Parse(s.SiteURL)
		if err == nil && (u.Scheme == "http" || u.Scheme == "https") && strings.EqualFold(u.Hostname(), host) {
			matches = append(matches, s.SiteURL)
		}
	}
	sort.Strings(matches)
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no property you can access matches %q; run gsc sites list", input)
	default:
		return "", fmt.Errorf("%q matches several properties (%s); pass one exactly with --site", input, strings.Join(matches, ", "))
	}
}

// PreferredDefault picks the default site after login: the only property,
// else the first domain property, else the first property.
func PreferredDefault(sites []client.SiteEntry) string {
	if len(sites) == 0 {
		return ""
	}
	sorted := append([]client.SiteEntry(nil), sites...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].SiteURL < sorted[j].SiteURL })
	for _, s := range sorted {
		if strings.HasPrefix(s.SiteURL, "sc-domain:") && s.PermissionLevel != "siteUnverifiedUser" {
			return s.SiteURL
		}
	}
	for _, s := range sorted {
		if s.PermissionLevel != "siteUnverifiedUser" {
			return s.SiteURL
		}
	}
	return sorted[0].SiteURL
}

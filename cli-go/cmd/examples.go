package cmd

import (
	"strings"

	"github.com/spf13/cobra"
)

// examples fills in help examples for commands whose constructors do not set
// one. It is keyed by command path because some constructors back several
// commands (auth status is also config show and status), and each needs
// examples that name the command being read.
var examples = map[string][]string{
	"gsc auth list":                  {"gsc auth list", "gsc auth list -o json"},
	"gsc auth logout":                {"gsc auth logout", "gsc auth logout --profile work --revoke --yes"},
	"gsc auth status":                {"gsc auth status", "gsc auth status --verify -o json"},
	"gsc status":                     {"gsc status", "gsc status --verify"},
	"gsc auth token":                 {`curl -H "Authorization: Bearer $(gsc auth token)" https://searchconsole.googleapis.com/webmasters/v3/sites`, "gsc auth token --profile ci"},
	"gsc auth use":                   {"gsc auth use work"},
	"gsc config list-profiles":       {"gsc config list-profiles", "gsc config list-profiles -o json"},
	"gsc config use-profile":         {"gsc config use-profile work"},
	"gsc config show":                {"gsc config show", "gsc config show --verify -o json"},
	"gsc completion":                 {"source <(gsc completion zsh)", "gsc completion bash > /usr/local/etc/bash_completion.d/gsc", "gsc completion fish > ~/.config/fish/completions/gsc.fish"},
	"gsc doctor":                     {"gsc doctor", "gsc doctor --online -o json"},
	"gsc freshness":                  {"gsc freshness", "gsc freshness --type discover -o json"},
	"gsc version":                    {"gsc version", "gsc version -o json"},
	"gsc commands":                   {"gsc commands", `gsc commands -o json | jq -r '.commands[] | select(.effect != "read") | .command'`},
	"gsc sites list":                 {"gsc sites list", "gsc sites list -o json"},
	"gsc sites get":                  {"gsc sites get", "gsc sites get https://www.example.com/ -o json"},
	"gsc sites use":                  {"gsc sites use sc-domain:example.com", "gsc sites use www.example.com"},
	"gsc sites add":                  {"gsc sites add https://blog.example.com/", "gsc sites add sc-domain:example.org --dry-run"},
	"gsc sites remove":               {"gsc sites remove https://old.example.com/ --yes"},
	"gsc sitemaps list":              {"gsc sitemaps list", "gsc sitemaps list --index https://www.example.com/sitemap_index.xml -o json"},
	"gsc sitemaps get":               {"gsc sitemaps get https://www.example.com/sitemap.xml"},
	"gsc sitemaps submit":            {"gsc sitemaps submit https://www.example.com/sitemap.xml", "gsc sitemaps submit https://www.example.com/sitemap.xml --dry-run"},
	"gsc sitemaps delete":            {"gsc sitemaps delete https://www.example.com/old-sitemap.xml --yes"},
	"gsc top queries":                {"gsc top queries --last 7d", "gsc top queries --compare previous --limit 50 -o json"},
	"gsc top pages":                  {"gsc top pages --filter 'page ~ /blog/'", "gsc top pages --all -o csv > pages.csv"},
	"gsc top countries":              {"gsc top countries --last 3m"},
	"gsc top devices":                {"gsc top devices --compare yoy"},
	"gsc top appearance":             {"gsc top appearance --last 3m -o json"},
	"gsc insights striking-distance": {"gsc insights striking-distance", "gsc insights striking-distance --min-impressions 500 --max-position 15 -o json"},
	"gsc insights low-ctr":           {"gsc insights low-ctr", "gsc insights low-ctr --factor 0.4 --min-impressions 200 -o json"},
	"gsc insights cannibalization":   {"gsc insights cannibalization --last 3m", "gsc insights cannibalization --min-share 0.2 --filter 'query ~ pricing'"},
	"gsc insights decliners":         {"gsc insights decliners --compare yoy", "gsc insights decliners --by page --min-clicks 20 -o json"},
	"gsc insights new-queries":       {"gsc insights new-queries --last 7d", "gsc insights new-queries --by page --compare yoy"},
	"gsc insights lost-queries":      {"gsc insights lost-queries --last 7d", "gsc insights lost-queries --by page -o json"},
}

func addExamples(c *cobra.Command) {
	for _, sub := range c.Commands() {
		addExamples(sub)
	}
	if ex, ok := examples[c.CommandPath()]; ok && c.Example == "" {
		c.Example = "  " + strings.Join(ex, "\n  ")
	}
}

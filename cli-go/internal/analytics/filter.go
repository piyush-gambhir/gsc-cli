package analytics

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
)

var Dimensions = []string{"date", "hour", "query", "page", "country", "device", "searchAppearance"}

var filterListDimensions = []string{"country", "device"}

var filterDimensions = []string{"query", "page", "country", "device", "searchAppearance"}

var Operators = []struct{ Token, API string }{
	{"!=~", "excludingRegex"},
	{"=~", "includingRegex"},
	{"!=", "notEquals"},
	{"!~", "notContains"},
	{"=", "equals"},
	{"~", "contains"},
}

func CanonicalDimension(name string, allowed []string) (string, bool) {
	for _, d := range allowed {
		if strings.EqualFold(name, d) {
			return d, true
		}
	}
	return "", false
}

func ParseFilter(s string) (client.Filter, error) {
	s = strings.TrimSpace(s)
	i := strings.IndexAny(s, " \t=!~")
	if i <= 0 {
		return client.Filter{}, fmt.Errorf("invalid --filter %q; use DIM OP VALUE, for example 'query ~ shoes'", s)
	}
	dim, ok := CanonicalDimension(s[:i], filterDimensions)
	if !ok {
		return client.Filter{}, fmt.Errorf("cannot filter on %q; filterable dimensions are %s", s[:i], strings.Join(filterDimensions, ", "))
	}
	rest := strings.TrimLeft(s[i:], " \t")
	var op string
	for _, o := range Operators {
		if strings.HasPrefix(rest, o.Token) {
			op, rest = o.API, rest[len(o.Token):]
			break
		}
	}
	if op == "" {
		return client.Filter{}, fmt.Errorf("invalid operator in --filter %q; use = != ~ !~ =~ !=~", s)
	}
	value := strings.TrimSpace(rest)
	if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
		value = value[1 : len(value)-1]
	}
	if value == "" {
		return client.Filter{}, fmt.Errorf("--filter %q has no value", s)
	}
	if op == "includingRegex" || op == "excludingRegex" {
		if _, err := regexp.Compile(value); err != nil {
			return client.Filter{}, fmt.Errorf("invalid RE2 regular expression in --filter %q: %v", s, err)
		}
	}
	return client.Filter{Dimension: dim, Operator: op, Expression: value}, nil
}

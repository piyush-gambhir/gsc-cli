package analytics

import (
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// Fuzz targets for user input parsed before any request: the --filter DSL,
// YYYY-MM-DD dates, and --last spans. `go test` runs the seeds;
// `go test -run=^$ -fuzz=FuzzParseFilter ./internal/analytics` explores further.

func FuzzParseFilter(f *testing.F) {
	for _, s := range []string{"query ~ running shoes", "page =~ ^https://example.com/blog/", "country = ind", "device != MOBILE",
		"query !~ 'free'", `page !=~ "\.pdf$"`, "searchAppearance = NEWS_SHOWCASE", "query=~(", "query", "= x", "QUERY ~ x", "query ~ \"\""} {
		f.Add(s)
	}
	apiOps := map[string]string{}
	for _, o := range Operators {
		apiOps[o.API] = o.Token
	}
	f.Fuzz(func(t *testing.T, s string) {
		got, err := ParseFilter(s)
		if err != nil {
			return
		}
		if !slices.Contains(filterDimensions, got.Dimension) || apiOps[got.Operator] == "" || got.Expression == "" {
			t.Fatalf("%q parsed to an invalid filter %+v", s, got)
		}
		if got.Operator == "includingRegex" || got.Operator == "excludingRegex" {
			if _, err := regexp.Compile(got.Expression); err != nil {
				t.Fatalf("%q accepted an invalid regex: %v", s, err)
			}
		}
		// Rendering the filter back as DSL parses to the same filter, unless the
		// value only survives because of trimming or quote stripping.
		v := got.Expression
		if strings.TrimSpace(v) != v || len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			return
		}
		again, err := ParseFilter(got.Dimension + " " + apiOps[got.Operator] + " " + v)
		if err != nil || again != got {
			t.Fatalf("%q -> %+v does not round-trip: %+v %v", s, got, again, err)
		}
	})
}

func FuzzParseDate(f *testing.F) {
	for _, s := range []string{"2026-10-03", "2024-02-29", "2026-02-29", "0000-01-01", "9999-12-31", "2026-1-3", "2026-13-01", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		d, err := ParseDate(s)
		if err != nil {
			return
		}
		if d.String() != s {
			t.Fatalf("%q parsed to %s", s, d)
		}
	})
}

// FuzzLastRange checks that every accepted --last span is a non-empty range
// ending at end, with the exact length for day and week spans.
func FuzzLastRange(f *testing.F) {
	for _, s := range []string{"7d", "28d", "4w", "3m", "16m", "1y", "0d", "9999d", "9999w", "9999m", "9999y", "10000d", "9223372036854775807d", "1537228672809129302w", "768614336404564651y", "x"} {
		f.Add(s, int64(0))
	}
	f.Fuzz(func(t *testing.T, spec string, offset int64) {
		end := DateOf(time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)).AddDays(int(offset % 4000))
		r, err := LastRange(spec, end)
		if err != nil {
			return
		}
		if r.End != end || r.End.Before(r.Start) {
			t.Fatalf("--last %s ending %s gave %s..%s", spec, end, r.Start, r.End)
		}
		n, unit := spec[:len(spec)-1], spec[len(spec)-1]
		want := map[byte]int{'d': 1, 'w': 7}[unit]
		if want != 0 && r.Days() != want*atoi(t, n) {
			t.Fatalf("--last %s gave %d days", spec, r.Days())
		}
		for _, mode := range []string{"previous", "yoy"} {
			p, err := ComparePeriod(r, mode)
			if err != nil || p.End.Before(p.Start) || !p.End.Before(r.End) {
				t.Fatalf("compare %s of %s..%s gave %s..%s (%v)", mode, r.Start, r.End, p.Start, p.End, err)
			}
		}
	})
}

func atoi(t *testing.T, s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

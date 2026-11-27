package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequestFileRowLimitStandsUnlessLimitIsGiven(t *testing.T) {
	isolate(t)
	t.Setenv("GSC_ACCESS_TOKEN", "env-token")
	file := filepath.Join(t.TempDir(), "req.json")
	if err := os.WriteFile(file, []byte(`{"startDate":"2026-09-01","endDate":"2026-09-30","dimensions":["query"],"rowLimit":5}`), 0600); err != nil {
		t.Fatal(err)
	}
	f := newFake(t)
	r := cli(t, f, now, "", "query", "-s", "sc-domain:example.com", "--request-file", file, "--print-request", "-o", "json")
	if r.code != 0 || !strings.Contains(r.out, `"rowLimit": 5`) {
		t.Fatalf("file rowLimit overridden by the default --limit: %s %s", r.out, r.errOut)
	}
	r = cli(t, f, now, "", "query", "-s", "sc-domain:example.com", "--request-file", file, "--limit", "7", "--print-request", "-o", "json")
	if r.code != 0 || !strings.Contains(r.out, `"rowLimit": 7`) {
		t.Fatalf("explicit --limit must override the file: %s %s", r.out, r.errOut)
	}
}

func TestRequestFileRejectsUnknownFields(t *testing.T) {
	isolate(t)
	t.Setenv("GSC_ACCESS_TOKEN", "env-token")
	f := newFake(t)
	for name, body := range map[string]string{
		"searchType": `{"startDate":"2026-09-01","endDate":"2026-09-30","searchType":"image","dimensions":["query"]}`,
		"bogus":      `{"startDate":"2026-09-01","endDate":"2026-09-30","bogus":1}`,
		"trailing":   `{"startDate":"2026-09-01","endDate":"2026-09-30"} {}`,
	} {
		file := filepath.Join(t.TempDir(), "req.json")
		if err := os.WriteFile(file, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		r := cli(t, f, now, "", "query", "-s", "sc-domain:example.com", "--request-file", file, "-o", "json")
		if r.code == 0 || !strings.Contains(r.errOut, "--request-file is not a valid") || !strings.Contains(r.errOut, `"kind": "usage"`) {
			t.Fatalf("%s was not rejected: %d %s %s", name, r.code, r.out, r.errOut)
		}
		if name == "searchType" {
			if !strings.Contains(r.errOut, `searchType is deprecated; use \"type\"`) || strings.Contains(r.errOut, "json: unknown field") {
				t.Fatalf("searchType error: %s", r.errOut)
			}
		} else if name != "trailing" && !strings.Contains(r.errOut, `unknown field \"`+name+`\"`) {
			t.Fatalf("%s: error does not name the field: %s", name, r.errOut)
		}
	}
	if f.total() != 0 {
		t.Fatalf("a rejected request file reached the network: %+v", f.calls)
	}
}

func TestEndAloneEndsTheDefaultWindow(t *testing.T) {
	isolate(t)
	t.Setenv("GSC_ACCESS_TOKEN", "env-token")
	f := newFake(t)
	r := cli(t, f, now, "", "query", "-s", "sc-domain:example.com", "-d", "query", "--end", "2026-09-20", "-o", "json")
	if r.code != 0 {
		t.Fatalf("--end alone: %s", r.errOut)
	}
	req := firstBody(t, f, "/searchAnalytics/query")
	if req.StartDate != "2026-08-24" || req.EndDate != "2026-09-20" || len(req.Dimensions) != 1 || req.Dimensions[0] != "query" {
		t.Fatalf("--end alone must cover 28 days ending there without the latest-date probe: %+v", req)
	}
}

func TestRequestFilterListsPassThrough(t *testing.T) {
	isolate(t)
	t.Setenv("GSC_ACCESS_TOKEN", "env-token")
	f := newFake(t)
	file := filepath.Join(t.TempDir(), "req.json")
	body := `{"startDate":"2026-09-01","endDate":"2026-09-30","dimensionFilterGroups":[{"filterLists":[{"dimension":"Country","expressions":["usa","ind"]}]}]}`
	if err := os.WriteFile(file, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	r := cli(t, f, now, "", "query", "-s", "sc-domain:example.com", "--request-file", file, "-o", "json")
	if r.code != 0 {
		t.Fatalf("filterLists rejected: %s", r.errOut)
	}
	req := firstBody(t, f, "/searchAnalytics/query")
	g := req.DimensionFilterGroups
	if len(g) != 1 || len(g[0].Filters) != 0 || len(g[0].FilterLists) != 1 || g[0].FilterLists[0].Dimension != "country" || len(g[0].FilterLists[0].Expressions) != 2 {
		t.Fatalf("filterLists not passed through: %+v", g)
	}
	bad := strings.Replace(body, "Country", "query", 1)
	if err := os.WriteFile(file, []byte(bad), 0600); err != nil {
		t.Fatal(err)
	}
	r = cli(t, f, now, "", "query", "-s", "sc-domain:example.com", "--request-file", file, "-o", "json")
	if r.code == 0 {
		t.Fatalf("filterLists on query must be rejected: %s", r.out)
	}
}

func TestEndOverridesRequestFileEndDate(t *testing.T) {
	isolate(t)
	t.Setenv("GSC_ACCESS_TOKEN", "env-token")
	f := newFake(t)
	file := filepath.Join(t.TempDir(), "req.json")
	if err := os.WriteFile(file, []byte(`{"startDate":"2026-09-01","endDate":"2026-09-30"}`), 0600); err != nil {
		t.Fatal(err)
	}
	r := cli(t, f, now, "", "query", "-s", "sc-domain:example.com", "--request-file", file, "--end", "2026-09-20", "-o", "json")
	if r.code != 0 {
		t.Fatalf("--end with request file startDate: %s", r.errOut)
	}
	req := firstBody(t, f, "/searchAnalytics/query")
	if req.StartDate != "2026-09-01" || req.EndDate != "2026-09-20" {
		t.Fatalf("unexpected range: %+v", req)
	}
}

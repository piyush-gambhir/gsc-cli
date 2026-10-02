package output

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestFormatsPreserveTypesAndPrecision(t *testing.T) {
	data := map[string]any{"id": json.Number("9007199254740993"), "numeric_string": "0012", "ok": true}
	for _, format := range []string{"json", "yaml"} {
		var b bytes.Buffer
		if err := Print(&b, format, data); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(b.String(), "9007199254740993") {
			t.Fatal("lost precision", b.String())
		}
		if format == "yaml" {
			var got map[string]any
			if err := yaml.Unmarshal(b.Bytes(), &got); err != nil || got["numeric_string"] != "0012" || got["ok"] != true {
				t.Fatalf("wrong types: %#v %v", got, err)
			}
		}
	}
}

type rows struct{}

func (rows) Table() Table {
	return Table{Columns: []string{"query", "ctr"}, Rows: [][]any{{"=HYPERLINK(\"x\")", 0.0249}, {"shoes, red", nil}}, Human: map[string]func(any) string{"ctr": Percent}}
}

func TestTablerTableAndCSV(t *testing.T) {
	var b bytes.Buffer
	if err := Print(&b, "table", rows{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "2.49%") || strings.Index(b.String(), "query") > strings.Index(b.String(), "ctr") {
		t.Fatalf("table: %s", b.String())
	}
	b.Reset()
	if err := Print(&b, "csv", rows{}); err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(&b).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if recs[0][0] != "query" || recs[1][0] != "'=HYPERLINK(\"x\")" || recs[1][1] != "0.0249" || recs[2][0] != "shoes, red" || recs[2][1] != "" {
		t.Fatalf("csv: %q", recs)
	}
}

func TestGenericCSVAndRejection(t *testing.T) {
	var b bytes.Buffer
	if err := Print(&b, "csv", []map[string]any{{"b": 2, "a": "x"}}); err != nil || b.String() != "a,b\nx,2\n" {
		t.Fatalf("%q %v", b.String(), err)
	}
	if err := Print(&b, "csv", map[string]any{"a": 1}); !errors.Is(err, ErrNoCSV) {
		t.Fatalf("expected ErrNoCSV, got %v", err)
	}
}

func TestTableEscapesControlsAndEmpty(t *testing.T) {
	var b bytes.Buffer
	if err := Print(&b, "table", []map[string]any{{"name": "\x1b[31mtest\nrow"}}); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(b.String(), '\x1b') {
		t.Fatal("terminal escape leaked")
	}
	b.Reset()
	if err := Print(&b, "table", []map[string]any{}); err != nil || strings.TrimSpace(b.String()) != "No results." {
		t.Fatalf("%q", b.String())
	}
	if err := Print(&b, "bad", nil); err == nil {
		t.Fatal("bad format accepted")
	}
}

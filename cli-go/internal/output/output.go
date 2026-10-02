package output

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode"

	"go.yaml.in/yaml/v3"
)

// Table is an ordered, row-shaped view of a result. Table output applies the
// Human formatters (for example CTR as a percentage); CSV prints raw values.
type Table struct {
	Columns []string
	Rows    [][]any
	Human   map[string]func(any) string
}

// Tabler is implemented by results with a natural row shape. JSON and YAML
// still serialize the result itself, so envelopes keep their metadata.
type Tabler interface{ Table() Table }

var ErrNoCSV = errors.New("csv output is only available for row-shaped results; use -o json or -o yaml")

func Valid(format string) bool {
	return format == "table" || format == "json" || format == "yaml" || format == "csv"
}

func Print(w io.Writer, format string, data any) error {
	if !Valid(format) {
		return fmt.Errorf("unsupported output %q; use table, json, yaml, or csv", format)
	}
	switch format {
	case "json":
		e := json.NewEncoder(w)
		e.SetIndent("", "  ")
		return e.Encode(data)
	case "yaml":
		return printYAML(w, data)
	case "csv":
		if t, ok := data.(Tabler); ok {
			return WriteCSV(w, t.Table())
		}
		normalized, err := normalize(data)
		if err != nil {
			return err
		}
		if t, ok := genericTable(normalized); ok {
			return WriteCSV(w, t)
		}
		return ErrNoCSV
	}
	if t, ok := data.(Tabler); ok {
		return writeTable(w, t.Table())
	}
	normalized, err := normalize(data)
	if err != nil {
		return err
	}
	if rows, ok := normalized.([]any); ok && len(rows) == 0 {
		_, err := fmt.Fprintln(w, "No results.")
		return err
	}
	if t, ok := genericTable(normalized); ok {
		return writeTable(w, t)
	}
	if m, ok := normalized.(map[string]any); ok {
		keys := sortedKeys(m)
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		for _, key := range keys {
			fmt.Fprintf(tw, "%s\t%s\n", Cell(key), Cell(m[key]))
		}
		return tw.Flush()
	}
	_, err = fmt.Fprintln(w, Cell(normalized))
	return err
}

// normalize round-trips typed structs through JSON while keeping numbers exact.
func normalize(data any) (any, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

func printYAML(w io.Writer, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	var node yaml.Node
	if err := yaml.Unmarshal(b, &node); err != nil {
		return err
	}
	var block func(*yaml.Node)
	block = func(n *yaml.Node) {
		n.Style = 0
		for _, child := range n.Content {
			block(child)
		}
	}
	block(&node)
	e := yaml.NewEncoder(w)
	e.SetIndent(2)
	defer e.Close()
	return e.Encode(&node)
}

// genericTable turns a list of objects into a table with sorted columns.
func genericTable(v any) (Table, bool) {
	rows, ok := v.([]any)
	if !ok || len(rows) == 0 {
		return Table{}, false
	}
	set := map[string]bool{}
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			return Table{}, false
		}
		for k := range m {
			set[k] = true
		}
	}
	t := Table{Columns: sortedKeys(set)}
	for _, row := range rows {
		m := row.(map[string]any)
		r := make([]any, len(t.Columns))
		for i, c := range t.Columns {
			r[i] = m[c]
		}
		t.Rows = append(t.Rows, r)
	}
	return t, true
}

func writeTable(w io.Writer, t Table) error {
	if len(t.Rows) == 0 {
		_, err := fmt.Fprintln(w, "No results.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	headers := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		headers[i] = Cell(c)
	}
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, row := range t.Rows {
		cells := make([]string, len(t.Columns))
		for i, c := range t.Columns {
			var v any
			if i < len(row) {
				v = row[i]
			}
			if f := t.Human[c]; f != nil && v != nil {
				cells[i] = Cell(f(v))
			} else {
				cells[i] = Cell(v)
			}
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	return tw.Flush()
}

// WriteCSV writes a table as RFC 4180 CSV with raw values.
func WriteCSV(w io.Writer, t Table) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(t.Columns); err != nil {
		return err
	}
	for _, row := range t.Rows {
		rec := make([]string, len(t.Columns))
		for i := range t.Columns {
			if i < len(row) {
				rec[i] = csvCell(row[i])
			}
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// csvCell prints raw values. Text that a spreadsheet would treat as a formula
// gets a leading apostrophe (OWASP CSV-injection guidance): search queries are
// attacker-controllable text.
func csvCell(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		if x != "" && strings.ContainsRune("=+-@\t\r", rune(x[0])) {
			return "'" + x
		}
		return x
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		return strconv.FormatBool(x)
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

// Cell renders a value for a terminal table, replacing control characters.
func Cell(v any) string {
	if v == nil {
		return "-"
	}
	var s string
	switch x := v.(type) {
	case string:
		s = x
	case float64:
		s = strconv.FormatFloat(x, 'f', -1, 64)
	default:
		b, _ := json.Marshal(v)
		s = string(b)
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Percent formats a ratio such as CTR for tables.
func Percent(v any) string {
	f, ok := toFloat(v)
	if !ok {
		return Cell(v)
	}
	return strconv.FormatFloat(f*100, 'f', 2, 64) + "%"
}

// Fixed2 formats a number with two decimals for tables.
func Fixed2(v any) string {
	f, ok := toFloat(v)
	if !ok {
		return Cell(v)
	}
	return strconv.FormatFloat(f, 'f', 2, 64)
}

// Points formats a value already expressed in percentage points.
func Points(v any) string {
	f, ok := toFloat(v)
	if !ok {
		return Cell(v)
	}
	return strconv.FormatFloat(f, 'f', 2, 64) + "pp"
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case *float64:
		if x == nil {
			return 0, false
		}
		return *x, true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}

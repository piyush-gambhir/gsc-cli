// Package analytics builds and post-processes Search Analytics queries:
// Pacific-time date ranges, the filter DSL, validation, pagination, period
// comparison, rollups, and insights.
package analytics

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
	_ "time/tzdata"
)

// Pacific is Search Console's reporting timezone (with daylight saving).
var Pacific = mustLoad("America/Los_Angeles")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

const DateLayout = "2006-01-02"

// Date is a calendar day in Search Console's timezone.
type Date struct{ t time.Time }

func DateOf(t time.Time) Date {
	y, m, d := t.In(Pacific).Date()
	return Date{time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
}

func ParseDate(s string) (Date, error) {
	t, err := time.Parse(DateLayout, s)
	if err != nil {
		return Date{}, fmt.Errorf("invalid date %q; use YYYY-MM-DD", s)
	}
	return Date{t}, nil
}

func (d Date) String() string       { return d.t.Format(DateLayout) }
func (d Date) AddDays(n int) Date   { return Date{d.t.AddDate(0, 0, n)} }
func (d Date) AddYears(n int) Date  { return d.AddMonths(12 * n) }
func (d Date) Before(o Date) bool   { return d.t.Before(o.t) }
func (d Date) After(o Date) bool    { return d.t.After(o.t) }
func (d Date) IsZero() bool         { return d.t.IsZero() }
func (d Date) Time() time.Time      { return d.t }
func (d Date) DaysUntil(o Date) int { return int(o.t.Sub(d.t).Hours() / 24) }

// AddMonths moves by calendar months, clamping to the last day of a shorter
// month (March 31 minus one month is February 28, not March 3).
func (d Date) AddMonths(n int) Date {
	y, m, day := d.t.Date()
	first := time.Date(y, m+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	if day > last {
		day = last
	}
	return Date{time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, time.UTC)}
}

// Range is an inclusive pair of days.
type Range struct{ Start, End Date }

func (r Range) Days() int { return r.Start.DaysUntil(r.End) + 1 }

var lastPattern = regexp.MustCompile(`^([1-9][0-9]*)([dwmy])$`)

// LastRange is the inclusive window of the given span ending at end: 28d ends
// at end and starts 27 days earlier; 3m starts the day after the same date
// three months before, or covers whole calendar months when end is a
// month's last day.
func LastRange(spec string, end Date) (Range, error) {
	m := lastPattern.FindStringSubmatch(spec)
	if m == nil {
		return Range{}, fmt.Errorf("invalid --last %q; use forms like 7d, 28d, 4w, 3m, 16m, 1y", spec)
	}
	n, _ := strconv.Atoi(m[1])
	var start Date
	switch m[2] {
	case "d":
		start = end.AddDays(-(n - 1))
	case "w":
		start = end.AddDays(-(7*n - 1))
	case "m", "y":
		months := n
		if m[2] == "y" {
			months = 12 * n
		}
		if end.AddDays(1).Time().Day() == 1 {
			// A month-end anchor covers whole calendar months: 3m ending
			// September 30 is July 1 to September 30.
			first := end.AddMonths(-(months - 1)).Time()
			start = Date{time.Date(first.Year(), first.Month(), 1, 0, 0, 0, 0, time.UTC)}
		} else {
			start = end.AddMonths(-months).AddDays(1)
		}
	}
	return Range{start, end}, nil
}

// RetentionStart is the oldest day Search Console keeps (16 months).
func RetentionStart(today Date) Date { return today.AddMonths(-16) }

// HourlyStart is the oldest day with hourly data (10 days).
func HourlyStart(today Date) Date { return today.AddDays(-10) }

// Clip trims a helper-generated range to data availability and reports
// whether it changed.
func Clip(r Range, oldest Date) (Range, bool) {
	if r.Start.Before(oldest) {
		return Range{oldest, r.End}, true
	}
	return r, false
}

// ComparePeriod returns the comparison range: the same-length window right
// before r ("previous"), or the same dates one year earlier ("yoy").
func ComparePeriod(r Range, mode string) (Range, error) {
	switch mode {
	case "previous":
		n := r.Days()
		return Range{r.Start.AddDays(-n), r.Start.AddDays(-1)}, nil
	case "yoy":
		return Range{r.Start.AddYears(-1), r.End.AddYears(-1)}, nil
	}
	return Range{}, fmt.Errorf("invalid --compare %q; use previous or yoy", mode)
}

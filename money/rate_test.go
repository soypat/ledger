package money

import (
	"testing"
	"time"

	"github.com/soypat/ledger/internal/fixed"
)

func TestRate(t *testing.T) {
	parse := []struct {
		s    string
		want Rate
		err  error
	}{
		{"21%", 210_000, nil},
		{"10.5%", 105_000, nil},
		{"0.21", 210_000, nil},
		{"130%", 1_300_000, nil},
		{"0.0001%", 1, nil},
		{"0%", 0, nil},
		{"0.00001%", 0, fixed.ErrInexact},
		{"0.0000001", 0, fixed.ErrInexact},
		{"-1%", 0, fixed.ErrRange},
		{"429497%", 0, fixed.ErrRange}, // 4_294_970_000 ppm passes a uint32
		{"%", 0, fixed.ErrSyntax},
		{"21%%", 0, fixed.ErrSyntax},
		{"", 0, fixed.ErrSyntax},
	}
	for _, c := range parse {
		got, err := ParseRate([]byte(c.s))
		if got != c.want || err != c.err {
			t.Errorf("ParseRate(%q) = %d, %v; want %d, %v", c.s, got, err, c.want, c.err)
		}
	}
	for _, r := range []Rate{0, 1, 210_000, 105_000, ppm_whole, 1_300_000, 4_294_967_295} {
		s := r.String()
		if got, err := ParseRate([]byte(s)); got != r || err != nil {
			t.Errorf("ParseRate(%q) = %d, %v; want %d", s, got, err, r)
		}
	}
	if got := Rate(210_000).String(); got != "21%" {
		t.Errorf("String = %q; want 21%%", got)
	}

	of := []struct {
		r      Rate
		amount uint64
		mode   Round
		want   uint64
		ok     bool
	}{
		{210_000, 100_000, RoundExact, 21_000, true},
		{210_000, 12_345, RoundHalfAway, 2_592, true}, // 2592.45
		{210_000, 12_345, RoundUp, 2_593, true},
		{210_000, 12_345, RoundExact, 0, false},
		{1_300_000, 1_000, RoundExact, 1_300, true},
		{ppm_whole, 1<<64 - 1, RoundExact, 1<<64 - 1, true},
		{ppm_whole + 1, 1<<64 - 1, RoundDown, 0, false},
	}
	for _, c := range of {
		got, ok := c.r.Of(c.amount, c.mode)
		if got != c.want || ok != c.ok {
			t.Errorf("Rate(%d).Of(%d, %v) = %d, %v; want %d, %v", c.r, c.amount, c.mode, got, ok, c.want, c.ok)
		}
	}
}

func TestMonth(t *testing.T) {
	m := MonthOf(time.Date(2026, time.November, 15, 13, 0, 0, 0, time.UTC))
	if m != 2026*12+10 {
		t.Fatalf("MonthOf(2026-11-15) = %d", m)
	}
	if got, want := m.Time(), time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("Time() = %v; want %v", got, want)
	}
	if got := (m + 2).Time(); got.Year() != 2027 || got.Month() != time.January {
		t.Errorf("two months after November 2026 = %v", got)
	}
	for _, mo := range []Month{-25, -13, -12, -1, 0, 1, 11, 12, 24299, 24300} {
		if got := MonthOf(mo.Time()); got != mo {
			t.Errorf("MonthOf(Month(%d).Time() = %v) = %d", mo, mo.Time(), got)
		}
	}
}

func TestIndex(t *testing.T) {
	jan := MonthOf(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))
	x := Index{Start: jan, Values: []uint64{100, 0, 150}}
	at := []struct {
		m    Month
		want uint64
		ok   bool
	}{
		{jan, 100, true},
		{jan + 1, 0, false}, // zero: not yet published
		{jan + 2, 150, true},
		{jan - 1, 0, false},
		{jan + 3, 0, false},
	}
	for _, c := range at {
		if got, ok := x.At(c.m); got != c.want || ok != c.ok {
			t.Errorf("At(%d) = %d, %v; want %d, %v", c.m-jan, got, ok, c.want, c.ok)
		}
	}
	adjust := []struct {
		amount   uint64
		from, to Month
		mode     Round
		want     uint64
		ok       bool
	}{
		{1000, jan, jan + 2, RoundExact, 1500, true},
		{1000, jan + 2, jan, RoundHalfAway, 667, true}, // 666.67
		{1000, jan + 2, jan, RoundDown, 666, true},
		{1000, jan + 2, jan, RoundExact, 0, false},
		{1000, jan, jan + 1, RoundExact, 0, false},
		{1000, jan - 1, jan, RoundExact, 0, false},
	}
	for _, c := range adjust {
		got, ok := x.Adjust(c.amount, c.from, c.to, c.mode)
		if got != c.want || ok != c.ok {
			t.Errorf("Adjust(%d, %d, %d, %v) = %d, %v; want %d, %v", c.amount, c.from-jan, c.to-jan, c.mode, got, ok, c.want, c.ok)
		}
	}
	mustPanic(t, "Adjust mode 0", func() { x.Adjust(1, jan, jan, 0) })
}

package money

import (
	"math"
	"testing"

	"github.com/soypat/ledger/fixed"
)

// cur is a caller-defined currency set, valued by ISO 4217 numeric code.
type cur uint32

const (
	ars cur = 32
	bhd cur = 48
	jpy cur = 392
	usd cur = 840
)

var curs = []cur{ars, bhd, jpy, usd}

func (c cur) Exp() int8 {
	switch c {
	case jpy:
		return 0
	case bhd:
		return -3
	}
	return -2
}

func (c cur) Ticker() string {
	switch c {
	case ars:
		return "ARS"
	case bhd:
		return "BHD"
	case jpy:
		return "JPY"
	case usd:
		return "USD"
	}
	return "???"
}
func TestFromFloat(t *testing.T) {
	cases := []struct {
		c    cur
		v    float64
		want uint64
	}{
		{ars, 120.00, 12000},
		{ars, 48_000.37, 4_800_037},
		{ars, 0, 0},
		{ars, 1e3, 100_000},
		{ars, 0.01, 1},
		{jpy, 1500, 1500},
		{bhd, 1.005, 1005},
		{ars, 1e15, 1e17},
	}
	for _, c := range cases {
		got, err := FromFloat(c.c, c.v)
		if err != nil || !got.IsValid() || got.Minor() != c.want || got.Currency() != c.c {
			t.Errorf("FromFloat(%s, %v) = %v, %v; want %d minor", c.c.Ticker(), c.v, got, err, c.want)
		}
	}
	tenth := 0.1 // constants add exactly at compile time, so compute at run time
	bad := []struct {
		name string
		c    cur
		v    float64
		err  error
	}{
		{"sub-minor digit", ars, 1.005, fixed.ErrInexact},
		{"negative", ars, -1, errNegative},
		{"negative zero is fine but -0.01 is not", ars, -0.01, errNegative},
		{"NaN", ars, math.NaN(), fixed.ErrRange},
		{"+Inf", ars, math.Inf(1), fixed.ErrRange},
		{"-Inf", ars, math.Inf(-1), fixed.ErrRange},
		{"past 2^64 minor units", ars, 1e30, fixed.ErrRange},
		{"yen with fraction", jpy, 1.5, fixed.ErrInexact},
		{"computed float", ars, tenth + 0.2, fixed.ErrInexact},
	}
	for _, c := range bad {
		if got, err := FromFloat(c.c, c.v); err != c.err || got.IsValid() {
			t.Errorf("FromFloat %s = %v, %v; want invalid, %v", c.name, got, err, c.err)
		}
	}
	if got, err := FromFloat(ars, math.Copysign(0, -1)); err != nil || !got.IsValid() || got.Minor() != 0 {
		t.Errorf("FromFloat(-0) = %v, %v; want 0", got, err)
	}
}

func TestAmountDecimal(t *testing.T) {
	if got := FromMinor(ars, 12037).Decimal(); got != MakeDecimal(12037, -2) {
		t.Errorf("120.37 ARS Decimal = %v", got)
	}
	if got := FromMinor(jpy, 1500).Decimal().String(); got != "1500" {
		t.Errorf("1500 JPY Decimal = %q", got)
	}
	if got := Sub(FromMinor(ars, 1), FromMinor(ars, 2)).Decimal(); got != MakeDecimal(0, -2) {
		t.Errorf("invalid Amount Decimal = %v; want 0", got)
	}
}

func TestAmountArithmetic(t *testing.T) {
	a, b := FromMinor(ars, 100), FromMinor(ars, 250)
	if s := Add(a, b); !s.IsValid() || s != FromMinor(ars, 350) {
		t.Errorf("Add = %+v", s)
	}
	if d := Sub(b, a); !d.IsValid() || d != FromMinor(ars, 150) {
		t.Errorf("Sub = %+v, %v", d, d.IsValid())
	}
	if ok := Sub(a, b).IsValid(); ok {
		t.Error("Sub below zero reported ok")
	}
	if p := a.Mul(3); !p.IsValid() || p != FromMinor(ars, 300) {
		t.Errorf("Mul = %+v, %v", p, p.IsValid())
	}
	top := FromMinor(ars, math.MaxUint64)
	if s := Add(top, FromMinor(ars, 0)); !s.IsValid() || s != top {
		t.Errorf("Add at the limit = %+v, %v", s, s.IsValid())
	}
	if ok := Add(top, FromMinor(ars, 1)).IsValid(); ok {
		t.Error("Add past 2^64-1 reported ok")
	}
	if p := FromMinor(ars, 1<<63).Mul(2); p.IsValid() {
		t.Errorf("Mul past 2^64-1 = %+v, true", p)
	}
	if p := top.Mul(1); !p.IsValid() || p != top {
		t.Errorf("Mul by 1 at the limit = %+v", p)
	}
	usd := FromMinor(usd, 1)
	mustPanic(t, "Add across currencies", func() { Add(a, usd) })
	mustPanic(t, "Sub across currencies", func() { Sub(a, usd) })
}

func TestAmountString(t *testing.T) {
	cases := []struct {
		a    Amount[cur]
		want string
	}{
		{FromMinor(ars, 12037), "120.37 ARS"},
		{FromMinor(usd, 1250), "12.50 USD"},
		{FromMinor(ars, 0), "0.00 ARS"},
		{FromMinor(jpy, 1500), "1500 JPY"},
		{FromMinor(bhd, 1005), "1.005 BHD"},
		{FromMinor(ars, math.MaxUint64), "184467440737095516.15 ARS"},
	}
	for _, c := range cases {
		if got := c.a.String(); got != c.want {
			t.Errorf("%+v.String() = %q; want %q", c.a, got, c.want)
		}
		num := c.want[:len(c.want)-4]
		if got, err := ParseDecimal([]byte(num)); err != nil || got != c.a.Decimal() {
			t.Errorf("ParseDecimal(%q) = %v, %v; want %v", num, got, err, c.a.Decimal())
		}
	}
	esAR := Grouping{Group: '.', Point: ','}
	formats := []struct {
		a    Amount[cur]
		g    Grouping
		want string
	}{
		{FromMinor(ars, 123456), esAR, "1.234,56 ARS"},
		{FromMinor(usd, 123456789), Grouping{Group: ',', Point: '.'}, "1,234,567.89 USD"},
		{FromMinor(jpy, 1500), esAR, "1.500 JPY"},
		{FromMinor(ars, 12037), Grouping{}, "120.37 ARS"},
		{Sub(FromMinor(ars, 1), FromMinor(ars, 2)), esAR, "invalid amount"},
	}
	for _, c := range formats {
		if got := string(c.a.AppendFormat([]byte("x="), c.g)); got != "x="+c.want {
			t.Errorf("%v.AppendFormat(%+v) = %q; want %q", c.a, c.g, got, "x="+c.want)
		}
	}
	dst := make([]byte, 0, 64)
	big := FromMinor(ars, math.MaxUint64)
	if n := testing.AllocsPerRun(100, func() { big.AppendFormat(dst[:0], esAR) }); n != 0 {
		t.Errorf("AppendFormat allocates %v times", n)
	}
}

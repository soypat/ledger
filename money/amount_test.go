package money

import (
	"math"
	"testing"
)

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
		if err != nil {
			panic(err)
		}
		if got.Minor() != c.want || got.Currency() != c.c {
			t.Errorf("FromFloat(%s, %v) = %+v;  want V=%d", c.c.Ticker(), c.v, got, c.want)
		}
	}
	bad := []struct {
		name string
		c    cur
		v    float64
	}{
		{"sub-minor digit", ars, 1.005},
		{"negative", ars, -1},
		{"negative zero is fine but -0.01 is not", ars, -0.01},
		{"NaN", ars, math.NaN()},
		{"+Inf", ars, math.Inf(1)},
		{"-Inf", ars, math.Inf(-1)},
		{"past 2^64 minor units", ars, 1e30},
		{"yen with fraction", jpy, 1.5},
	}
	for _, c := range bad {
		mustPanic(t, "FromFloat "+c.name, func() { FromFloat(c.c, c.v) })
	}
	tenth := 0.1 // constants add exactly at compile time, so compute at run time
	mustPanic(t, "FromFloat computed float", func() { FromFloat(ars, tenth+0.2) })
	if got, _ := FromFloat(ars, math.Copysign(0, -1)); got.Minor() != 0 {
		t.Errorf("FromFloat(-0) = %+v; want 0", got)
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
		if got, err := ParseAmount([]byte(c.want[:len(c.want)-4]), c.a.Currency(), RoundExact); err != nil || got != c.a.Minor() {
			t.Errorf("ParseAmount(%q) = %d, %v; want %d", c.want[:len(c.want)-4], got, err, c.a.Minor())
		}
	}
}

package iso4217

import (
	"math"
	"testing"

	"github.com/soypat/ledger/money"
)

// TestTable checks what find relies on and what every ISO 4217 code satisfies.
func TestTable(t *testing.T) {
	if len(table) != 178 {
		t.Errorf("table has %d currencies; list one of 2026-09-17 has 178", len(table))
	}
	seen := map[string]bool{}
	for i, e := range table {
		if i > 0 && table[i-1].c >= e.c {
			t.Errorf("%s: table not in ascending numeric order", e.ticker)
		}
		if e.c == 0 || e.c > 999 {
			t.Errorf("%s: numeric code %d outside 1..999", e.ticker, e.c)
		}
		if len(e.ticker) != 3 || seen[e.ticker] {
			t.Errorf("%q: ticker not three letters, or repeated", e.ticker)
		}
		for _, r := range e.ticker {
			if r < 'A' || r > 'Z' {
				t.Errorf("%q: ticker not upper case letters", e.ticker)
			}
		}
		seen[e.ticker] = true
		if e.exp > 0 || e.exp < -4 {
			t.Errorf("%s: exponent %d", e.ticker, e.exp)
		}
	}
}

func TestCurrency(t *testing.T) {
	// Values known apart from the table, its first and last rows among them.
	cases := []struct {
		c      Currency
		num    uint32
		ticker string
		exp    int8
	}{
		{ALL, 8, "ALL", -2}, {ARS, 32, "ARS", -2}, {CHF, 756, "CHF", -2}, {EUR, 978, "EUR", -2},
		{GBP, 826, "GBP", -2}, {USD, 840, "USD", -2},
		{JPY, 392, "JPY", 0}, {XOF, 952, "XOF", 0},
		{BHD, 48, "BHD", -3}, {KWD, 414, "KWD", -3},
		{CLF, 990, "CLF", -4}, {UYW, 927, "UYW", -4},
		{XAU, 959, "XAU", 0}, {XXX, 999, "XXX", 0}, // no minor unit in ISO 4217
	}
	for _, c := range cases {
		if uint32(c.c) != c.num || !c.c.Valid() || c.c.Ticker() != c.ticker || c.c.Exp() != c.exp {
			t.Errorf("%s: code %d valid %v ticker %q exp %d; want %d true %q %d",
				c.ticker, c.c, c.c.Valid(), c.c.Ticker(), c.c.Exp(), c.num, c.ticker, c.exp)
		}
	}

	// Every code up to 999 is valid exactly if the table holds it.
	valid := 0
	for c := range Currency(1000) {
		if c.Valid() {
			valid++
			if e := c.find(); e.c != c || c.Ticker() != e.ticker || c.Exp() != e.exp {
				t.Errorf("code %d: find gave %+v", c, *e)
			}
		}
	}
	if valid != len(table) {
		t.Errorf("%d valid codes, table has %d", valid, len(table))
	}

	for _, c := range []Currency{0, 1, 998, 1000, math.MaxUint32} {
		if c.Valid() || c.Ticker() != "" {
			t.Errorf("code %d: valid %v ticker %q; want invalid", c, c.Valid(), c.Ticker())
		}
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("code %d: Exp did not panic", c)
				}
			}()
			c.Exp()
		}()
	}

	if n := testing.AllocsPerRun(100, func() { _, _, _ = USD.Ticker(), USD.Exp(), USD.Valid() }); n != 0 {
		t.Errorf("Ticker, Exp and Valid allocate %v times", n)
	}
}

// TestMoney uses Currency as package money's currency type.
func TestMoney(t *testing.T) {
	parse := []struct {
		s    string
		c    Currency
		mode money.Round
		want uint64
	}{
		{"1234.56", USD, money.RoundExact, 123456},
		{"1234.56", JPY, money.RoundHalfEven, 1235},
		{"1.005", BHD, money.RoundExact, 1005},
		{"1.2345", CLF, money.RoundExact, 12345},
		{"3", XAU, money.RoundExact, 3},
	}
	for _, c := range parse {
		if got, err := money.ParseAmount([]byte(c.s), c.c, c.mode); got != c.want || err != nil {
			t.Errorf("ParseAmount(%q, %s) = %d, %v; want %d", c.s, c.c.Ticker(), got, err, c.want)
		}
		if c.mode != money.RoundExact {
			continue // text was rounded, so it does not come back
		}
		if got := string(money.AppendAmount(nil, c.want, c.c)); got != c.s {
			t.Errorf("AppendAmount(%d, %s) = %q; want %q", c.want, c.c.Ticker(), got, c.s)
		}
	}

	set := []Currency{ARS, EUR, USD}
	if got, err := money.ParseTicker([]byte("EUR"), set); got != EUR || err != nil {
		t.Errorf("ParseTicker(EUR) = %d, %v", got, err)
	}
	if _, err := money.ParseTicker([]byte("JPY"), set); err == nil {
		t.Error("ParseTicker(JPY) found a currency outside the set")
	}
}

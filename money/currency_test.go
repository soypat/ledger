package money

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/soypat/ledger/internal/fixed"
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

func TestAmount(t *testing.T) {
	parse := []struct {
		s    string
		c    cur
		mode Round
		want uint64
		err  error
	}{
		{"1234.56", usd, RoundExact, 123456, nil},
		{"1234.56", jpy, RoundExact, 0, fixed.ErrInexact},
		{"1234.56", jpy, RoundHalfEven, 1235, nil},
		{"1.005", usd, RoundExact, 0, fixed.ErrInexact},
		{"1.005", bhd, RoundExact, 1005, nil},
		{"12", bhd, RoundExact, 12000, nil},
		{"184467440737095516.15", usd, RoundExact, math.MaxUint64, nil},
		{"184467440737095516.16", usd, RoundExact, 0, fixed.ErrRange},
		{"-1", usd, RoundExact, 0, fixed.ErrRange},
		{"5k", usd, RoundExact, 0, fixed.ErrSyntax},
	}
	for _, c := range parse {
		got, err := ParseAmount([]byte(c.s), c.c, c.mode)
		if got != c.want || err != c.err {
			t.Errorf("ParseAmount(%q, %v, %v) = %d, %v; want %d, %v", c.s, c.c.Ticker(), c.mode, got, err, c.want, c.err)
		}
	}
	mustPanic(t, "ParseAmount mode 0", func() { ParseAmount([]byte("1"), usd, 0) })

	appends := []struct {
		v    uint64
		c    cur
		want string
	}{
		{123456, usd, "1234.56"},
		{1250, usd, "12.50"},
		{5, usd, "0.05"},
		{0, usd, "0.00"},
		{1235, jpy, "1235"},
		{1005, bhd, "1.005"},
		{math.MaxUint64, usd, "184467440737095516.15"},
	}
	for _, c := range appends {
		if got := string(AppendAmount(nil, c.v, c.c)); got != c.want {
			t.Errorf("AppendAmount(%d, %v) = %q; want %q", c.v, c.c.Ticker(), got, c.want)
		}
	}

	rng := rand.New(rand.NewPCG(1, 2))
	var buf []byte
	for range 5000 {
		v := rng.Uint64() >> rng.IntN(64)
		c := curs[rng.IntN(len(curs))]
		buf = AppendAmount(buf[:0], v, c)
		if got, err := ParseAmount(buf, c, RoundExact); got != v || err != nil {
			t.Fatalf("ParseAmount(AppendAmount(%d, %v) = %q) = %d, %v", v, c.Ticker(), buf, got, err)
		}
	}

	b := []byte("1234.567")
	dst := make([]byte, 0, 32)
	if n := testing.AllocsPerRun(100, func() { ParseAmount(b, usd, RoundHalfEven) }); n != 0 {
		t.Errorf("ParseAmount allocates %v times", n)
	}
	if n := testing.AllocsPerRun(100, func() { ParseAmount(b, usd, RoundExact) }); n != 0 {
		t.Errorf("ParseAmount failing allocates %v times", n)
	}
	if n := testing.AllocsPerRun(100, func() { AppendAmount(dst[:0], math.MaxUint64, bhd) }); n != 0 {
		t.Errorf("AppendAmount allocates %v times", n)
	}
}

func TestParseTicker(t *testing.T) {
	for _, c := range curs {
		if got, err := ParseTicker([]byte(c.Ticker()), curs); got != c || err != nil {
			t.Errorf("ParseTicker(%q) = %d, %v", c.Ticker(), got, err)
		}
	}
	for _, s := range []string{"", "usd", "XXX", "USDX", "US"} {
		if _, err := ParseTicker([]byte(s), curs); err != errUnknownCurrency {
			t.Errorf("ParseTicker(%q) err = %v; want errUnknownCurrency", s, err)
		}
	}
	if _, err := ParseTicker[cur]([]byte("USD"), nil); err != errUnknownCurrency {
		t.Errorf("ParseTicker in an empty set err = %v", err)
	}
	b := []byte("JPY")
	if n := testing.AllocsPerRun(100, func() { ParseTicker(b, curs) }); n != 0 {
		t.Errorf("ParseTicker allocates %v times", n)
	}
}

package fixed

import (
	"math"
	"math/rand/v2"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		s    string
		exp  int8
		q    uint64
		neg  bool
		tail Tail
		err  Err
	}{
		{"1234.56", -2, 123456, false, TailZero, 0},
		{"-0.01", -2, 1, true, TailZero, 0},
		{"+7", 0, 7, false, TailZero, 0},
		{".5", -1, 5, false, TailZero, 0},
		{"5.", 0, 5, false, TailZero, 0},
		{"-0", -2, 0, true, TailZero, 0},
		{"1e3", 0, 1000, false, TailZero, 0},
		{"1.5E-1", -2, 15, false, TailZero, 0},
		{"2e+2", 2, 2, false, TailZero, 0},
		{"1.000", -2, 100, false, TailZero, 0}, // zeros below exp are no tail
		{"1200", 2, 12, false, TailZero, 0},
		{"0e100", 0, 0, false, TailZero, 0},
		{"000000000000000000000000000001", 0, 1, false, TailZero, 0},

		// Tail: the first digit cut off, and whether any later one is nonzero.
		{"0.005", -2, 0, false, TailHalf, 0},
		{"0.015", -2, 1, false, TailHalf, 0},
		{"0.02501", -2, 2, false, TailAbove, 0},
		{"0.0049999", -2, 0, false, TailBelow, 0},
		{"0.009", -2, 0, false, TailAbove, 0},
		{"0.001", -2, 0, false, TailBelow, 0},
		{"-0.005", -2, 0, true, TailHalf, 0},
		{"5", 2, 0, false, TailBelow, 0}, // 0.05 of the unit: every digit cut off
		{"50", 2, 0, false, TailHalf, 0},
		{"51", 2, 0, false, TailAbove, 0},
		{"1e-30", 0, 0, false, TailBelow, 0},
		{"0.0050000000000000000000000001", -2, 0, false, TailAbove, 0},
		{"0.123456789012345678901234567890", -2, 12, false, TailBelow, 0}, // a long tail does not overflow

		// Range.
		{"18446744073709551615", 0, math.MaxUint64, false, TailZero, 0},
		{"184467440737095516.15", -2, math.MaxUint64, false, TailZero, 0},
		{"18446744073709551616", 0, 0, false, 0, ErrRange},
		{"1e19", 0, 1e19, false, TailZero, 0},
		{"2e19", 0, 0, false, 0, ErrRange}, // the power-of-ten multiply overflows
		{"1e20", 0, 0, false, 0, ErrRange}, // past the power-of-ten table
		{"1e100000", 0, 0, false, 0, ErrRange},

		// Syntax.
		{"", -2, 0, false, 0, ErrSyntax},
		{"-", -2, 0, false, 0, ErrSyntax},
		{"+", -2, 0, false, 0, ErrSyntax},
		{".", -2, 0, false, 0, ErrSyntax},
		{"--1", -2, 0, false, 0, ErrSyntax},
		{"1..2", -2, 0, false, 0, ErrSyntax},
		{"1.2.3", -2, 0, false, 0, ErrSyntax},
		{"1,5", -2, 0, false, 0, ErrSyntax},
		{" 1", -2, 0, false, 0, ErrSyntax},
		{"1 ", -2, 0, false, 0, ErrSyntax},
		{"5k", -2, 0, false, 0, ErrSyntax},
		{"12.5abc", -2, 0, false, 0, ErrSyntax},
		{"e5", -2, 0, false, 0, ErrSyntax},
		{"1e", -2, 0, false, 0, ErrSyntax},
		{"1e+", -2, 0, false, 0, ErrSyntax},
		{"1e5x", -2, 0, false, 0, ErrSyntax},
		{"1e2.5", -2, 0, false, 0, ErrSyntax},
	}
	for _, c := range cases {
		q, neg, tail, err := Parse([]byte(c.s), c.exp)
		if q != c.q || neg != c.neg || tail != c.tail || err != c.err {
			t.Errorf("Parse(%q, %d) = %d, %v, %d, %d; want %d, %v, %d, %d",
				c.s, c.exp, q, neg, tail, err, c.q, c.neg, c.tail, c.err)
		}
	}
}

func TestExp(t *testing.T) {
	cases := []struct {
		s   string
		exp int
		err Err
	}{
		{"1.50", -2, 0},
		{"15", 0, 0},
		{"1e3", 3, 0},
		{"1.5e3", 2, 0},
		{".5", -1, 0},
		{"-0.001", -3, 0},
		{"x", 0, ErrSyntax},
		{"1e99999", 0, ErrRange},
	}
	for _, c := range cases {
		if exp, err := Exp([]byte(c.s)); exp != c.exp || err != c.err {
			t.Errorf("Exp(%q) = %d, %d; want %d, %d", c.s, exp, err, c.exp, c.err)
		}
	}
}

func TestPow10(t *testing.T) {
	for k, want := range pow10 {
		if got, ok := Pow10(k); got != want || !ok {
			t.Errorf("Pow10(%d) = %d, %v", k, got, ok)
		}
	}
	for _, k := range []int{-1, 20, 100} {
		if _, ok := Pow10(k); ok {
			t.Errorf("Pow10(%d) ok", k)
		}
	}
}

func TestAppend(t *testing.T) {
	cases := []struct {
		u    uint64
		exp  int8
		trim bool
		want string
	}{
		{123456, -2, true, "1234.56"},
		{100, -2, true, "1"},
		{150, -2, true, "1.5"},
		{0, -2, true, "0"},
		{7, -1, true, "0.7"},
		{5, 0, true, "5"},
		{5, 2, true, "500"},
		{0, 2, true, "0"},
		{1, -18, true, "0.000000000000000001"},
		{math.MaxUint64, -19, true, "1.8446744073709551615"},
		{math.MaxUint64, -20, true, "0.18446744073709551615"},
		{150, -2, false, "1.50"},
		{100, -2, false, "1.00"},
		{0, -2, false, "0.00"},
		{5, -3, false, "0.005"},
		{5, 0, false, "5"},
		{5, 2, false, "500"},
	}
	for _, c := range cases {
		if got := string(Append(nil, c.u, c.exp, c.trim)); got != c.want {
			t.Errorf("Append(%d, %d, %v) = %q; want %q", c.u, c.exp, c.trim, got, c.want)
		}
	}
	if got := string(Append([]byte("$"), 250, -2, true)); got != "$2.5" {
		t.Errorf("Append onto a prefix = %q", got)
	}
}

func TestRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	var buf []byte
	for range 20000 {
		u := rng.Uint64()
		if rng.IntN(2) == 0 {
			u >>= rng.IntN(64) // small magnitudes too
		}
		exp := int8(rng.IntN(32) - 25) // -25 through 6
		trim := rng.IntN(2) == 0
		buf = Append(buf[:0], u, exp, trim)
		q, neg, tail, err := Parse(buf, exp)
		if q != u || neg || tail != TailZero || err != 0 {
			t.Fatalf("Parse(Append(%d, %d, %v) = %q) = %d, %v, %d, %d", u, exp, trim, buf, q, neg, tail, err)
		}
	}
}

func TestAllocs(t *testing.T) {
	b := []byte("-1234567.891e-1")
	dst := make([]byte, 0, 64)
	if n := testing.AllocsPerRun(100, func() { Parse(b, -2) }); n != 0 {
		t.Errorf("Parse allocates %v times", n)
	}
	if n := testing.AllocsPerRun(100, func() { Parse(b[:3], -2) }); n != 0 {
		t.Errorf("Parse failing allocates %v times", n)
	}
	if n := testing.AllocsPerRun(100, func() { Append(dst[:0], math.MaxUint64, -7, false) }); n != 0 {
		t.Errorf("Append allocates %v times", n)
	}
}

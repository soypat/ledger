package fixed

import (
	"math"
	"math/rand/v2"
	"testing"
)

func TestSplit(t *testing.T) {
	cases := []struct {
		s    string
		exp  int8
		q    uint64
		neg  bool
		tail tail
		err  Err
	}{
		{"1234.56", -2, 123456, false, tailZero, 0},
		{"-0.01", -2, 1, true, tailZero, 0},
		{"+7", 0, 7, false, tailZero, 0},
		{".5", -1, 5, false, tailZero, 0},
		{"5.", 0, 5, false, tailZero, 0},
		{"-0", -2, 0, true, tailZero, 0},
		{"1e3", 0, 1000, false, tailZero, 0},
		{"1.5E-1", -2, 15, false, tailZero, 0},
		{"2e+2", 2, 2, false, tailZero, 0},
		{"1.000", -2, 100, false, tailZero, 0}, // zeros below exp are no tail
		{"1200", 2, 12, false, tailZero, 0},
		{"0e100", 0, 0, false, tailZero, 0},
		{"000000000000000000000000000001", 0, 1, false, tailZero, 0},

		// Tail: the first digit cut off, and whether any later one is nonzero.
		{"0.005", -2, 0, false, tailHalf, 0},
		{"0.015", -2, 1, false, tailHalf, 0},
		{"0.02501", -2, 2, false, tailAbove, 0},
		{"0.0049999", -2, 0, false, tailBelow, 0},
		{"0.009", -2, 0, false, tailAbove, 0},
		{"0.001", -2, 0, false, tailBelow, 0},
		{"-0.005", -2, 0, true, tailHalf, 0},
		{"5", 2, 0, false, tailBelow, 0}, // 0.05 of the unit: every digit cut off
		{"50", 2, 0, false, tailHalf, 0},
		{"51", 2, 0, false, tailAbove, 0},
		{"1e-30", 0, 0, false, tailBelow, 0},
		{"0.0050000000000000000000000001", -2, 0, false, tailAbove, 0},
		{"0.123456789012345678901234567890", -2, 12, false, tailBelow, 0}, // a long tail does not overflow

		// Range.
		{"18446744073709551615", 0, math.MaxUint64, false, tailZero, 0},
		{"184467440737095516.15", -2, math.MaxUint64, false, tailZero, 0},
		{"18446744073709551616", 0, 0, false, 0, ErrRange},
		{"1e19", 0, 1e19, false, tailZero, 0},
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
		q, neg, tl, err := split([]byte(c.s), c.exp)
		if q != c.q || neg != c.neg || tl != c.tail || err != c.err {
			t.Errorf("split(%q, %d) = %d, %v, %d, %d; want %d, %v, %d, %d",
				c.s, c.exp, q, neg, tl, err, c.q, c.neg, c.tail, c.err)
		}
	}
}

func TestParse(t *testing.T) {
	cases := []struct {
		s    string
		exp  int8
		mode Round
		want uint64
		err  Err
	}{
		{"1234.56", -2, RoundExact, 123456, 0},
		{"0.005", -2, RoundHalfAway, 1, 0},
		{"0.005", -2, RoundHalfEven, 0, 0},
		{"0.015", -2, RoundHalfEven, 2, 0},
		{"0.025", -2, RoundHalfEven, 2, 0},
		{"0.02501", -2, RoundHalfEven, 3, 0},
		{"0.0049999", -2, RoundHalfAway, 0, 0},
		{"0.001", -2, RoundUp, 1, 0},
		{"0.009", -2, RoundDown, 0, 0},
		{"0.005", -2, RoundExact, 0, ErrInexact},
		{"5", 2, RoundHalfAway, 0, 0}, // 0.05 of the unit
		{"5", 2, RoundUp, 1, 0},
		{"50", 2, RoundHalfAway, 1, 0},
		{"50", 2, RoundHalfEven, 0, 0},
		{"1e-30", 0, RoundHalfAway, 0, 0},
		{"1e-30", 0, RoundExact, 0, ErrInexact},
		{"18446744073709551615", 0, RoundExact, math.MaxUint64, 0},
		{"18446744073709551615.4", 0, RoundHalfAway, math.MaxUint64, 0},
		{"18446744073709551615.5", 0, RoundHalfAway, 0, ErrRange}, // rounding up wraps
		{"18446744073709551616", 0, RoundExact, 0, ErrRange},
		{"1e100000", 0, RoundExact, 0, ErrRange},
		{"-0", -2, RoundExact, 0, 0},
		{"-0.00", -2, RoundExact, 0, 0},
		{"-0.001", -2, RoundDown, 0, ErrRange}, // negative, though it rounds to zero
		{"-1", -2, RoundExact, 0, ErrRange},
		{"1.2.3", -2, RoundExact, 0, ErrSyntax},
	}
	for _, c := range cases {
		got, err := Parse([]byte(c.s), c.exp, c.mode)
		if got != c.want || err != c.err {
			t.Errorf("Parse(%q, %d, %v) = %d, %v; want %d, %v", c.s, c.exp, c.mode, got, err, c.want, c.err)
		}
	}
	mustPanic(t, "Parse mode 0", func() { Parse([]byte("1"), 0, 0) })
	for err, want := range map[Err]string{ErrSyntax: "invalid syntax", ErrRange: "value out of range", ErrInexact: "inexact result"} {
		if got := err.Error(); got != want {
			t.Errorf("Err(%d).Error() = %q; want %q", err, got, want)
		}
	}
}

func TestMulDiv(t *testing.T) {
	// (2^65-1)/31 times 31 is 2^65-1 = 2·(2^64-1) + 1: half by 2 is 2^64-1 rem 1.
	const a = (1<<65 - 1) / 31
	cases := []struct {
		a, num, den uint64
		mode        Round
		want        uint64
		err         Err
	}{
		{10, 1, 3, RoundDown, 3, 0},
		{10, 1, 3, RoundUp, 4, 0},
		{10, 1, 3, RoundHalfAway, 3, 0},
		{10, 1, 3, RoundHalfEven, 3, 0},
		{10, 1, 3, RoundExact, 0, ErrInexact},
		{9, 1, 3, RoundExact, 3, 0},
		{5, 1, 2, RoundHalfAway, 3, 0},
		{5, 1, 2, RoundHalfEven, 2, 0},
		{7, 1, 2, RoundHalfEven, 4, 0},
		{5, 1, 2, RoundDown, 2, 0},
		{11, 1, 4, RoundHalfAway, 3, 0}, // 2.75
		{9, 1, 4, RoundHalfAway, 2, 0},  // 2.25
		{0, 7, 3, RoundExact, 0, 0},
		{math.MaxUint64, math.MaxUint64, math.MaxUint64, RoundExact, math.MaxUint64, 0},
		{math.MaxUint64, 2, 1, RoundDown, 0, ErrRange},
		{math.MaxUint64, 3, 2, RoundDown, 0, ErrRange},
		{a, 31, 2, RoundDown, math.MaxUint64, 0},
		{a, 31, 2, RoundUp, 0, ErrRange},       // rounding up passes 2^64-1
		{a, 31, 2, RoundHalfAway, 0, ErrRange}, // at half: up
		{a, 31, 2, RoundHalfEven, 0, ErrRange}, // 2^64-1 is odd: up
	}
	for _, c := range cases {
		got, err := MulDiv(c.a, c.num, c.den, c.mode)
		if got != c.want || err != c.err {
			t.Errorf("MulDiv(%d, %d, %d, %v) = %d, %v; want %d, %v", c.a, c.num, c.den, c.mode, got, err, c.want, c.err)
		}
	}
	mustPanic(t, "den 0", func() { MulDiv(1, 1, 0, RoundDown) })
	mustPanic(t, "mode 0", func() { MulDiv(1, 1, 1, 0) })
	if n := testing.AllocsPerRun(100, func() { MulDiv(12345, 7, 3, RoundHalfEven) }); n != 0 {
		t.Errorf("MulDiv allocates %v times", n)
	}
}

func TestCheckRound(t *testing.T) {
	for m := RoundExact; m < roundMax; m++ {
		CheckRound(m)
	}
	mustPanic(t, "Round 0", func() { CheckRound(0) })
	mustPanic(t, "Round past last", func() { CheckRound(roundMax) })
	if got := RoundHalfEven.String(); got != "half even" {
		t.Errorf("RoundHalfEven.String() = %q", got)
	}
}

func mustPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: no panic", name)
		}
	}()
	f()
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
		q, err := Parse(buf, exp, RoundExact)
		if q != u || err != 0 {
			t.Fatalf("Parse(Append(%d, %d, %v) = %q) = %d, %v", u, exp, trim, buf, q, err)
		}
	}
}

func TestAllocs(t *testing.T) {
	b := []byte("1234567.891e-1")
	dst := make([]byte, 0, 64)
	if n := testing.AllocsPerRun(100, func() { Parse(b, -2, RoundHalfEven) }); n != 0 {
		t.Errorf("Parse allocates %v times", n)
	}
	if n := testing.AllocsPerRun(100, func() { Parse(b, -2, RoundExact) }); n != 0 {
		t.Errorf("Parse failing allocates %v times", n)
	}
	if n := testing.AllocsPerRun(100, func() { Append(dst[:0], math.MaxUint64, -7, false) }); n != 0 {
		t.Errorf("Append allocates %v times", n)
	}
}

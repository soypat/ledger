package money

import (
	"math"
	"testing"

	"github.com/soypat/ledger/fixed"
)

func mustPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: no panic", name)
		}
	}()
	f()
}

// TestMulDiv checks the wrapper; fixed tests the arithmetic.
func TestMulDiv(t *testing.T) {
	if got, ok := mulDiv(10, 1, 3, RoundHalfEven); got != 3 || !ok {
		t.Errorf("MulDiv(10, 1, 3) = %d, %v; want 3, true", got, ok)
	}
	if _, ok := mulDiv(10, 1, 3, RoundExact); ok {
		t.Error("MulDiv(10, 1, 3, RoundExact) ok")
	}
	if _, ok := mulDiv(math.MaxUint64, 2, 1, RoundDown); ok {
		t.Error("MulDiv overflow ok")
	}
	mustPanic(t, "MulDiv mode 0", func() { mulDiv(1, 1, 1, 0) })
	if got := RoundHalfAway.String(); got != "half away" {
		t.Errorf("RoundHalfAway.String() = %q", got)
	}
	if got := errNoQuote.Error(); got != "no quote for currency pair" {
		t.Errorf("errNoQuote.Error() = %q", got)
	}
	if got := errNegative.Error(); got != "negative unrepresentable in a ledger" {
		t.Errorf("errNegative.Error() = %q", got)
	}
	if got := errInvalidAmount.Error(); got != "invalid amount" {
		t.Errorf("errInvalidAmount.Error() = %q", got)
	}
}

func TestDecimal(t *testing.T) {
	cases := []struct {
		s    string
		want Decimal
		err  error
	}{
		{"1185.50", Decimal{118550, -2}, nil},
		{"1185.5", Decimal{11855, -1}, nil},
		{"15", Decimal{15, 0}, nil},
		{"1.5e3", Decimal{1500, 0}, nil},
		{"1e-3", Decimal{1, -3}, nil},
		{"0.00084388", Decimal{84388, -8}, nil},
		{"0", Decimal{0, 0}, nil},
		{"18446744073709551615", Decimal{math.MaxUint64, 0}, nil},
		{"18446744073709551616", Decimal{}, fixed.ErrRange},
		{"1e-200", Decimal{}, fixed.ErrRange}, // exponent past an int8
		{"1e99999", Decimal{}, fixed.ErrRange},
		{"-1", Decimal{}, fixed.ErrRange},
		{"x", Decimal{}, fixed.ErrSyntax},
	}
	for _, c := range cases {
		got, err := ParseDecimal([]byte(c.s))
		if got != c.want || err != c.err {
			t.Errorf("ParseDecimal(%q) = %v, %v; want %v, %v", c.s, got, err, c.want, c.err)
		}
	}
	for _, s := range []string{"1185.50", "0.00084388", "15", "0.001", "0"} {
		d, _ := ParseDecimal([]byte(s))
		if got := d.String(); got != s {
			t.Errorf("ParseDecimal(%q).String() = %q", s, got)
		}
		if v, exp := d.Parts(); MakeDecimal(v, exp) != d {
			t.Errorf("MakeDecimal(%d, %d) != ParseDecimal(%q)", v, exp, s)
		}
	}
	if got := MakeDecimal(15, 2).String(); got != "1500" {
		t.Errorf("MakeDecimal(15, 2) = %q; want 1500", got)
	}
	if a, b := MakeDecimal(11855, -1), MakeDecimal(118550, -2); a == b {
		t.Errorf("%v == %v; == compares digits, not values", a, b)
	}
	// fixed tests the grouping rule; these check Decimal passes Grouping through.
	formats := []struct {
		d    Decimal
		g    Grouping
		want string
	}{
		{MakeDecimal(123456, -2), Grouping{Group: '.', Point: ','}, "1.234,56"},
		{MakeDecimal(84388, -8), Grouping{Group: '.', Point: ','}, "0,00084388"},
		{MakeDecimal(1234567, 0), Grouping{Group: '\''}, "1'234'567"},
		{MakeDecimal(123456, -2), Grouping{}, "1234.56"},
	}
	for _, c := range formats {
		if got := string(c.d.AppendFormat(nil, c.g)); got != c.want {
			t.Errorf("%v.AppendFormat(%+v) = %q; want %q", c.d, c.g, got, c.want)
		}
	}
}

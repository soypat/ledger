package money

import (
	"math"
	"testing"
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
	if got, ok := MulDiv(10, 1, 3, RoundHalfEven); got != 3 || !ok {
		t.Errorf("MulDiv(10, 1, 3) = %d, %v; want 3, true", got, ok)
	}
	if _, ok := MulDiv(10, 1, 3, RoundExact); ok {
		t.Error("MulDiv(10, 1, 3, RoundExact) ok")
	}
	if _, ok := MulDiv(math.MaxUint64, 2, 1, RoundDown); ok {
		t.Error("MulDiv overflow ok")
	}
	mustPanic(t, "MulDiv mode 0", func() { MulDiv(1, 1, 1, 0) })
	if got := RoundHalfAway.String(); got != "half away" {
		t.Errorf("RoundHalfAway.String() = %q", got)
	}
	if got := errNoQuote.Error(); got != "no quote for currency pair" {
		t.Errorf("errNoQuote.Error() = %q", got)
	}
}

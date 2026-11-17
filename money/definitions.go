// Package money does exact arithmetic on amounts in minor units (cents, centavos):
// decimal text, rounding, rates, currency exchange, proportional splits and price-index
// adjustment. There is no float64: every result is exact or rounded by a [Round] the
// caller names, and nothing allocates beyond memory the caller passes.
//
// Amounts are uint64 minor units, as in package ledger, which money does not import.
// Currencies are the caller's own type; see [Currency].
package money

import "github.com/soypat/ledger/internal/fixed"

// Add all enum-like types to money.go and to the following go:generate command:
//go:generate stringer -type=moneyErr -linecomment -output stringers.go

// moneyErr is why an operation failed. Returning one does not allocate.
type moneyErr uint8

const (
	_                  moneyErr = iota
	errUnknownCurrency          // unknown currency
	errNoQuote                  // no quote for currency pair
	errCapacity                 // capacity exceeded
	errNegative                 // negative unrepresentable in a ledger
)

func (e moneyErr) Error() string { return e.String() }

// Round names a rounding rule. The zero value is invalid, so callers always choose;
// functions taking a Round panic on an invalid one. Rules act on magnitudes.
type Round = fixed.Round

const (
	RoundExact    = fixed.RoundExact    // fail instead of rounding: an error, or false
	RoundDown     = fixed.RoundDown     // toward zero
	RoundUp       = fixed.RoundUp       // away from zero
	RoundHalfAway = fixed.RoundHalfAway // to nearest, half away from zero
	RoundHalfEven = fixed.RoundHalfEven // to nearest, half to even
)

// mulDiv returns a·num/den rounded by mode, exact through a 128-bit product; false if
// the result passes 2^64-1, or mode is RoundExact and the division is inexact.
// It panics on den == 0 or an invalid mode.
func mulDiv(a, num, den uint64, mode Round) (uint64, bool) {
	v, err := fixed.MulDiv(a, num, den, mode)
	return v, err == 0
}

// result returns a fixed result as money's, with a nil error on success: a zero
// fixed.Err in an error interface would not be nil.
func result(v uint64, err fixed.Err) (uint64, error) {
	if err != 0 {
		return 0, err
	}
	return v, nil
}

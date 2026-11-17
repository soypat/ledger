// Package money does exact arithmetic on amounts in minor units (cents, centavos):
// decimal text, rounding, rates, currency exchange, proportional splits and price-index
// adjustment. There is no float64: every result is exact or rounded by a [Round] the
// caller names, and nothing allocates beyond memory the caller passes.
//
// Amounts are uint64 minor units, as in package ledger, which money does not import.
// Currencies are the caller's own type; see [Currency].
package money

import (
	"math"

	"github.com/soypat/ledger/fixed"
)

// Add all enum-like types to money.go and to the following go:generate command:
//go:generate stringer -type=moneyErr -linecomment -output stringers.go

// Currency is a set of currencies the caller defines, such as
//
//	type Currency uint32
//
//	const (
//		ARS Currency = 32  // ISO 4217 numeric codes make good ledger numbers;
//		USD Currency = 840 // 0 is not one, as the ledger rejects it.
//	)
//
// A value is the ledger number of the accounts that hold it: Account.Ledger and
// Transfer.Ledger in package ledger. Money needs only a currency's exponent and ticker;
// conversion between currencies is [Exchange]. Package money/iso4217 has every
// ISO 4217 currency ready-made.
type Currency interface {
	~uint32
	// Exp is the minor-unit exponent, fixed per value: -2 for cents, 0 for JPY,
	// -3 for BHD.
	Exp() int8
	// Ticker is the code naming the currency in text, such as "USD". Return a
	// constant, so that calling it does not allocate.
	Ticker() string
}

// moneyErr is why an operation failed. Returning one does not allocate.
type moneyErr uint8

const (
	_                  moneyErr = iota
	errUnknownCurrency          // unknown currency
	errNoQuote                  // no quote for currency pair
	errCapacity                 // capacity exceeded
	errNegative                 // negative unrepresentable in a ledger
	errInvalidAmount            // invalid amount
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

// Grouping is how decimal text is written for display: Group between every three
// integer digits and Point before the fraction. es-AR is {'.', ','} and en-US is
// {',', '.'}. A zero Group writes no marks and a zero Point writes '.', so the zero
// Grouping writes plain text, as String does.
type Grouping struct {
	Group, Point byte
}

// Decimal is an exact decimal v·10^exp, for numbers quoted with more digits than a
// currency's minor unit, such as an exchange rate (see [Quote]). It keeps every digit it
// was given, so == compares digits, not values: 1185.5 and 1185.50 are not ==. The zero
// Decimal is 0.
type Decimal struct {
	V   uint64
	Exp int8
}

// ParseDecimal parses decimal text such as "1185.50" exactly, keeping every digit:
// MakeDecimal(118550, -2). The exponent is that of the last digit but at most 0, so
// "1.5e3" is MakeDecimal(1500, 0). An error on anything but an optional sign, digits
// with at most one point, and an optional e or E exponent, or if negative, past
// 2^64-1, or the exponent would pass an int8.
func ParseDecimal(b []byte) (Decimal, error) {
	e, err := fixed.Exp(b)
	switch {
	case err != 0:
		return Decimal{}, err
	case e < math.MinInt8:
		return Decimal{}, fixed.ErrRange
	}
	exp := int8(min(e, 0))
	v, err := fixed.Parse(b, exp, RoundExact) // keeps every digit, so it is never inexact
	if err != 0 {
		return Decimal{}, err
	}
	return Decimal{V: v, Exp: exp}, nil
}

// AppendFormat appends d to dst with all its digits, written as g says:
// MakeDecimal(123456, -2) is "1.234,56" for es-AR.
func (d Decimal) AppendFormat(dst []byte, g Grouping) []byte {
	return fixed.Append(dst, d.V, d.Exp, false, g.Group, g.Point)
}

// StringAppend appends d to dst with all its digits and no grouping:
// MakeDecimal(118550, -2) is "1185.50". [ParseDecimal] inverts it.
func (d Decimal) StringAppend(dst []byte) []byte { return d.AppendFormat(dst, Grouping{}) }

func (d Decimal) String() string { return string(d.StringAppend(nil)) }

package money

import "github.com/soypat/ledger/internal/fixed"

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
// conversion between currencies is [Rates] and [Exchange].
type Currency interface {
	~uint32
	// Exp is the minor-unit exponent, fixed per value: -2 for cents, 0 for JPY,
	// -3 for BHD.
	Exp() int8
	// Ticker is the code naming the currency in text, such as "USD". Return a
	// constant, so that calling it does not allocate.
	Ticker() string
}

// ParseAmount parses decimal text in major units of c, such as "1234.56" or "1.5e3",
// into minor units: 123456 for USD. Digits below c's minor unit round by mode, and
// RoundExact reports them as ErrInexact. ErrSyntax on anything but an optional sign,
// digits with at most one point, and an optional e or E exponent; ErrRange if negative
// or past 2^64-1. It panics on an invalid mode.
func ParseAmount[C Currency](b []byte, c C, mode Round) (uint64, error) {
	mode.check()
	return parse(b, c.Exp(), mode)
}

// AppendAmount appends v minor units of c to dst in major units, with every minor
// digit: 123456 USD is "1234.56" and 1250 is "12.50". [ParseAmount] inverts it.
func AppendAmount[C Currency](dst []byte, v uint64, c C) []byte {
	return fixed.Append(dst, v, c.Exp(), false)
}

// ParseTicker returns the currency in set whose Ticker is b; ErrUnknownCurrency if none.
func ParseTicker[C Currency](b []byte, set []C) (C, error) {
	for _, c := range set {
		if string(b) == c.Ticker() {
			return c, nil
		}
	}
	return 0, ErrUnknownCurrency
}

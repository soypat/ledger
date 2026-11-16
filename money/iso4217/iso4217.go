// Package iso4217 is the ISO 4217 currency list as a currency set for package money.
// A [Currency] is a currency's numeric code and gives its minor-unit exponent and its
// alphabetic code, which is all money.Currency asks of a currency type.
//
// The numeric code also serves as the ledger number of the accounts that hold the
// currency: Account.Ledger and Transfer.Ledger in package ledger. No code is zero,
// which the ledger rejects.
package iso4217

// Currency is an ISO 4217 numeric code, such as 840 for the US dollar. The constants
// name every active one by its alphabetic code; any other value is invalid, see
// [Currency.Valid].
type Currency uint32

// entry is one currency of the list.
type entry struct {
	c      Currency
	exp    int8   // minor-unit exponent; zero where ISO 4217 gives no minor unit
	ticker string // alphabetic code
}

// Valid reports whether c is the numeric code of an active ISO 4217 currency.
func (c Currency) Valid() bool { return c.find() != nil }

// Exp returns the minor-unit exponent: -2 for USD, 0 for JPY, -3 for BHD, -4 for CLF.
// It is 0 for the codes ISO 4217 gives no minor unit, such as XAU and XDR, whose amounts
// are then whole units. It panics if c is not valid, as a wrong exponent would misread
// every amount by a power of ten.
func (c Currency) Exp() int8 {
	e := c.find()
	if e == nil {
		panic("iso4217: Exp of an invalid Currency")
	}
	return e.exp
}

// Ticker returns the alphabetic code, such as "USD"; empty if c is not valid.
// It does not allocate.
func (c Currency) Ticker() string {
	e := c.find()
	if e == nil {
		return ""
	}
	return e.ticker
}

// find returns c's entry of the table by binary search; nil if c is not valid.
func (c Currency) find() *entry {
	lo, hi := 0, len(table)
	for lo < hi {
		mid := (lo + hi) / 2
		if table[mid].c < c {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < len(table) && table[lo].c == c {
		return &table[lo]
	}
	return nil
}

package money

import (
	"math"
	"strconv"

	"github.com/soypat/ledger/internal/fixed"
)

// Amount is a count of minor units of a currency: 12037 ARS is 120.37 ARS. It is
// immutable: FromMinor and FromFloat make one, and arithmetic returns a new one. The zero
// Amount has no currency. The currency is usually the ledger number of the accounts holding it.
type Amount[C Currency] struct {
	v  uint64
	c  C
	ok bool // false for the zero Amount, or after an operation overflowed or underflowed
}

// FromMinor returns v minor units of c, for amounts computed or parsed at run time:
// FromMinor(ARS, 12037) is 120.37 ARS.
func FromMinor[C Currency](c C, v uint64) Amount[C] { return Amount[C]{v: v, c: c, ok: true} }

// Minor returns a's count of minor units. If invalid returns 0.
func (a Amount[C]) Minor() uint64 {
	if !a.ok {
		return 0
	}
	return a.v
}

// Currency returns a's currency.
func (a Amount[C]) Currency() C { return a.c }

// IsValid returns true if Amount was created with valid value and if operations done on it did not under/overflow.
func (a Amount[C]) IsValid() bool { return a.ok }

// Decimal returns a in major units: 12037 minor ARS is 120.37. Like Minor, it is 0 if
// a is invalid.
func (a Amount[C]) Decimal() Decimal {
	return Decimal{v: a.Minor(), exp: a.c.Exp()}
}

// FromFloat returns v major units of c, for amounts written as source literals:
// FromFloat(ARS, 120.37) is 12037 minor units of ARS. The literal's own digits are what
// count, so 48_000.37 is exact while 1.005 in a currency of cents is not. An error if v
// is NaN, infinite, negative, inexact in c's minor unit, or past 2^64-1 of them; amounts
// read at run time go through ParseAmount.
func FromFloat[C Currency](c C, v float64) (Amount[C], error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return Amount[C]{}, fixed.ErrRange
	} else if v < 0 {
		return Amount[C]{}, errNegative
	}
	// The shortest decimal that round-trips to v is the literal that produced it.
	var buf [32]byte
	b := strconv.AppendFloat(buf[:0], v, 'f', -1, 64)
	u, err := fixed.Parse(b, c.Exp(), RoundExact)
	if err != 0 {
		return Amount[C]{}, err
	}
	return Amount[C]{v: u, c: c, ok: true}, nil
}

// Add returns a+b, invalid if it passes 2^64-1. It panics if the currencies differ.
func Add[C Currency](a, b Amount[C]) Amount[C] {
	a.sameCurrency(b, "Add")
	s := a.v + b.v
	return Amount[C]{v: s, c: a.c, ok: a.ok && b.ok && s >= a.v}
}

// Sub returns a-b, invalid if b exceeds a. It panics if the currencies differ.
func Sub[C Currency](a, b Amount[C]) Amount[C] {
	a.sameCurrency(b, "Sub")
	return Amount[C]{v: a.v - b.v, c: a.c, ok: a.ok && b.ok && b.v <= a.v}
}

// Mul returns a times n, invalid if it passes 2^64-1.
func (a Amount[C]) Mul(n uint64) Amount[C] {
	return a.MulDiv(n, 1)
}

// MulDiv returns a times numerator divided by denominator, exact through a 128-bit
// product; invalid if it passes 2^64-1 or the division is inexact. It panics if
// denominator is 0.
func (a Amount[C]) MulDiv(numerator, denominator uint64) Amount[C] {
	v, ok := mulDiv(a.v, numerator, denominator, RoundExact)
	return Amount[C]{v: v, c: a.c, ok: a.ok && ok}
}

func (a Amount[C]) sameCurrency(b Amount[C], op string) {
	if a.c != b.c {
		panic("money: Amount." + op + " of " + a.c.Ticker() + " and " + b.c.Ticker())
	}
}

// StringAppend appends a as its major units and ticker, "120.37 ARS", with every minor
// digit as AppendAmount writes them. ParseAmount reads the number back.
func (a Amount[C]) StringAppend(dst []byte) []byte {
	if !a.ok {
		return append(dst, "invalid amount"...)
	}
	dst = fixed.Append(dst, a.v, a.c.Exp(), false)
	dst = append(dst, ' ')
	return append(dst, a.c.Ticker()...)
}

func (a Amount[C]) String() string { return string(a.StringAppend(nil)) }

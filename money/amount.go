package money

import (
	"math"
	"math/bits"
	"strconv"

	"github.com/soypat/ledger/fixed"
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
	return Decimal{V: a.Minor(), Exp: a.c.Exp()}
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
	hi, lo := bits.Mul64(a.v, n)
	return Amount[C]{v: lo, c: a.c, ok: a.ok && hi == 0}
}

// MulDiv returns a times numerator divided by denominator, rounded by mode, exact through
// a 128-bit product: a share of an amount, or an amount spread over units. Invalid, with
// the error, if it passes 2^64-1 or mode is RoundExact and the division is inexact;
// invalid too if a is. It panics if denominator is 0 or on an invalid mode.
func (a Amount[C]) MulDiv(numerator, denominator uint64, mode Round) (Amount[C], fixed.Err) {
	v, err := fixed.MulDiv(a.v, numerator, denominator, mode)
	return Amount[C]{v: v, c: a.c, ok: a.ok && err == 0}, err
}

// RoundTo returns a rounded by mode to a multiple of step, such as a price up to the next
// hundred: invalid, with the error, if it passes 2^64-1 or mode is RoundExact and a is no
// multiple of step; invalid too if a or step is. It panics if the currencies differ, if
// step is zero or on an invalid mode.
func (a Amount[C]) RoundTo(step Amount[C], mode Round) (Amount[C], fixed.Err) {
	a.sameCurrency(step, "RoundTo")
	v, err := fixed.RoundTo(a.v, step.v, mode)
	return Amount[C]{v: v, c: a.c, ok: a.ok && step.ok && err == 0}, err
}

func (a Amount[C]) sameCurrency(b Amount[C], op string) {
	if a.c != b.c {
		panic("money: Amount." + op + " of " + a.c.Ticker() + " and " + b.c.Ticker())
	}
}

// AppendFormat appends a as its major units, with every minor digit and written as g
// says, then its ticker: "1.234,56 ARS" for es-AR. An invalid a is "invalid amount".
func (a Amount[C]) AppendFormat(dst []byte, g Grouping) []byte {
	if !a.ok {
		return append(dst, "invalid amount"...)
	}
	dst = a.Decimal().AppendFormat(dst, g)
	dst = append(dst, ' ')
	return append(dst, a.c.Ticker()...)
}

// StringAppend appends a as its major units, with every minor digit and no grouping,
// then its ticker: "120.37 ARS". [ParseDecimal] reads the number back as a.Decimal().
func (a Amount[C]) StringAppend(dst []byte) []byte { return a.AppendFormat(dst, Grouping{}) }

func (a Amount[C]) String() string { return string(a.StringAppend(nil)) }

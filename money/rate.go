package money

import (
	"math"
	"time"

	"github.com/soypat/ledger/internal/fixed"
)

// Rate is a fraction in parts per million: 210_000 is 21%. It may exceed PPM, as a
// 130% markup does.
type Rate uint32

// ppm_whole is the Rate of a whole: 100%.
const (
	ppm_whole Rate = 1_000_000
	Percent   Rate = ppm_whole / 100
)

// Of returns amount·r rounded by mode; false if it passes 2^64-1, or mode is RoundExact
// and the result is inexact. It panics on an invalid mode.
func (r Rate) Of(amount uint64, mode Round) (uint64, bool) {
	return mulDiv(amount, uint64(r), uint64(ppm_whole), mode)
}

// Gross returns net·(1+r) rounded by mode: an amount with tax or markup r added. False
// if it passes 2^64-1, or mode is RoundExact and the result is inexact. It panics on an
// invalid mode.
func (r Rate) Gross(net uint64, mode Round) (uint64, bool) {
	return mulDiv(net, r.plusWhole(), uint64(ppm_whole), mode)
}

// Net returns gross/(1+r) rounded by mode: the amount before tax or markup r was added.
// The tax is gross less Net, so the two always sum to gross, while Gross of Net may
// differ from gross by rounding. It never passes gross; false if mode is RoundExact and
// the result is inexact. It panics on an invalid mode.
func (r Rate) Net(gross uint64, mode Round) (uint64, bool) {
	return mulDiv(gross, uint64(ppm_whole), r.plusWhole(), mode)
}

// plusWhole returns 1+r in parts per million, in 64 bits so that no Rate overflows it.
func (r Rate) plusWhole() uint64 { return uint64(ppm_whole) + uint64(r) }

// Compound returns amount·(1+r)^n, rounding by mode after each of the n periods, as a
// contract settled every period does. A negative n discounts: amount/(1+r)^-n, rounded
// the same way. False if it passes 2^64-1, or mode is RoundExact and a period is
// inexact. It panics on an invalid mode.
//
// It takes one step per period, stopping early once a period leaves the amount
// unchanged, as every later one then would.
func (r Rate) Compound(amount uint64, n int, mode Round) (uint64, bool) {
	fixed.CheckRound(mode)
	step, towardZero := Rate.Gross, -1
	if n < 0 {
		step, towardZero = Rate.Net, 1
	}
	for ; n != 0; n += towardZero {
		next, ok := step(r, amount, mode)
		if !ok {
			return 0, false
		}
		if next == amount {
			break
		}
		amount = next
	}
	return amount, true
}

// ParseRate parses a percentage such as "21%" or "10.5%", or a fraction such as "0.21".
// An error below one part per million, if negative, or past a uint32.
func ParseRate(b []byte) (Rate, error) {
	exp := int8(-6) // a fraction: 0.21 is 210_000 ppm
	if n := len(b); n > 0 && b[n-1] == '%' {
		b, exp = b[:n-1], -4 // a percentage: 21 is 210_000 ppm
	}
	v, err := fixed.Parse(b, exp, RoundExact)
	switch {
	case err != 0:
		return 0, err
	case v > math.MaxUint32:
		return 0, fixed.ErrRange
	}
	return Rate(v), nil
}

// StringAppend appends r as a percentage, "21%", to dst. [ParseRate] inverts it.
func (r Rate) StringAppend(dst []byte) []byte {
	return append(fixed.Append(dst, uint64(r), -4, true), '%')
}

func (r Rate) String() string { return string(r.StringAppend(nil)) }

// Month counts months since January of year 0, so month arithmetic is integer
// arithmetic: m+12 is a year after m.
type Month int32

// MonthOf returns the month of t, in t's location.
func MonthOf(t time.Time) Month {
	y, m, _ := t.Date()
	return Month(y*12 + int(m) - 1)
}

// Time returns the first instant of m, in UTC.
func (m Month) Time() time.Time {
	y, mo := int(m)/12, int(m)%12
	if mo < 0 { // floor division for years before 0
		y, mo = y-1, mo+12
	}
	return time.Date(y, time.Month(mo+1), 1, 0, 0, 0, 0, time.UTC)
}

// Index is a monthly price index such as IPC or CAC: Values[i] is its value for month
// Start+i, in any fixed-point scale, since only ratios are used. Values is the
// caller's memory; reload it in place with x.Values = append(x.Values[:0], ...).
type Index struct {
	Start  Month
	Values []uint64
}

// At returns the index value for m; false outside the series or where its value is
// zero, which marks a month not yet published.
func (x *Index) At(m Month) (uint64, bool) {
	i := int64(m) - int64(x.Start)
	if i < 0 || i >= int64(len(x.Values)) || x.Values[i] == 0 {
		return 0, false
	}
	return x.Values[i], true
}

// Adjust returns amount·x(to)/x(from) rounded by mode: amount at from's prices,
// expressed at to's. False if either month has no value, the result passes 2^64-1, or
// mode is RoundExact and it is inexact. It panics on an invalid mode.
func (x *Index) Adjust(amount uint64, from, to Month, mode Round) (uint64, bool) {
	fixed.CheckRound(mode)
	f, okf := x.At(from)
	t, okt := x.At(to)
	if !okf || !okt {
		return 0, false
	}
	return mulDiv(amount, t, f, mode)
}

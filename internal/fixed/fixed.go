// Package fixed reads and writes ASCII decimals as unsigned integers at a fixed
// exponent: q at exponent -2 counts hundredths. It does not round. [Parse] truncates
// and reports what it cut off as a [Tail], so callers apply their own rounding rule.
package fixed

import (
	"math"
	"math/bits"
	"strconv"
)

// Err is why a parse failed; the zero value is success.
type Err uint8

const (
	_         Err = iota
	ErrSyntax     // anything but a sign, digits with at most one point, and an e exponent
	ErrRange      // a magnitude past 2^64-1, or an e exponent past maxSci
)

// Tail is what [Parse] cut off below the unit of its exponent, against half a unit.
type Tail uint8

const (
	TailZero  Tail = iota // nothing, or only zeros
	TailBelow             // below half a unit
	TailHalf              // exactly half a unit
	TailAbove             // above half a unit
)

// maxSci bounds the e exponent; past it every value overflows or is cut off.
const maxSci = 1 << 16

// pow10[k] is 10^k, up to the largest power of ten a uint64 holds.
var pow10 = [20]uint64{
	1, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9,
	1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19,
}

// Pow10 returns 10^k; false if k is negative or 10^k passes 2^64-1.
func Pow10(k int) (uint64, bool) {
	if k < 0 || k >= len(pow10) {
		return 0, false
	}
	return pow10[k], true
}

// number is a decimal split by its syntax.
type number struct {
	mantissa []byte // digits and at most one point, without sign or exponent
	n        int    // digits in mantissa
	nfrac    int    // digits after the point
	sci      int    // the e exponent
	neg      bool
}

func scan(b []byte) (num number, err Err) {
	i := 0
	if len(b) > 0 && (b[0] == '+' || b[0] == '-') {
		num.neg = b[0] == '-'
		i++
	}
	start := i
	dot := false
	for ; i < len(b) && b[i] != 'e' && b[i] != 'E'; i++ {
		switch c := b[i]; {
		case c >= '0' && c <= '9':
			num.n++
			if dot {
				num.nfrac++
			}
		case c == '.' && !dot:
			dot = true
		default:
			return number{}, ErrSyntax
		}
	}
	if num.n == 0 {
		return number{}, ErrSyntax
	}
	num.mantissa = b[start:i]
	if i < len(b) {
		if num.sci, err = parseSci(b[i+1:]); err != 0 {
			return number{}, err
		}
	}
	return num, 0
}

// parseSci parses the exponent after 'e': an optional sign and at least one digit.
func parseSci(b []byte) (int, Err) {
	i, neg := 0, false
	if len(b) > 0 && (b[0] == '+' || b[0] == '-') {
		neg = b[0] == '-'
		i++
	}
	if i == len(b) {
		return 0, ErrSyntax
	}
	v := 0
	for ; i < len(b); i++ {
		c := b[i]
		if c < '0' || c > '9' {
			return 0, ErrSyntax
		}
		v = v*10 + int(c-'0')
		if v > maxSci {
			return 0, ErrRange
		}
	}
	if neg {
		v = -v
	}
	return v, 0
}

// Exp returns the exponent of the last digit of ASCII decimal b: -2 for "1.50", 0 for
// "15", 2 for "1.5e3". It reports the same syntax errors as [Parse].
func Exp(b []byte) (int, Err) {
	num, err := scan(b)
	return num.sci - num.nfrac, err
}

// Parse reads ASCII decimal b, such as "-1234.56" or "1.5e3", as the magnitude q with
// q·10^exp equal to |b| truncated, and tail saying what was cut off: exp -2 counts
// hundredths. ErrSyntax on anything but an optional sign, digits with at most one
// point, and an optional e or E exponent; ErrRange if q passes 2^64-1.
func Parse(b []byte, exp int8) (q uint64, neg bool, tail Tail, err Err) {
	num, err := scan(b)
	if err != 0 {
		return 0, false, 0, err
	}
	// The value is D·10^s, D the mantissa's digits read as an integer. Its first keep
	// digits are at or above the unit of q; the rest are cut off.
	s := num.sci - num.nfrac - int(exp)
	keep := num.n + s
	var first byte // first digit cut off; stays 0 when keep < 0, as the implicit zeros are
	sticky := false
	j := 0
	for _, c := range num.mantissa {
		if c == '.' {
			continue
		}
		d := c - '0'
		switch {
		case j < keep:
			if q > (math.MaxUint64-uint64(d))/10 {
				return 0, false, 0, ErrRange
			}
			q = q*10 + uint64(d)
		case j == keep:
			first = d
		default:
			sticky = sticky || d != 0
		}
		j++
	}
	if s > 0 && q != 0 {
		m, ok := Pow10(s)
		hi, lo := bits.Mul64(q, m)
		if !ok || hi != 0 {
			return 0, false, 0, ErrRange
		}
		q = lo
	}
	switch {
	case first > 5 || first == 5 && sticky:
		tail = TailAbove
	case first == 5:
		tail = TailHalf
	case first != 0 || sticky:
		tail = TailBelow
	}
	return q, num.neg, tail, 0
}

// Append appends u·10^exp in decimal to dst: 123456 at exp -2 is "1234.56". With trim,
// trailing fractional zeros and a bare point are dropped ("1234.5", "12"); without, the
// fraction always has -exp digits ("1234.50", "12.00"). [Parse] at exp inverts it.
func Append(dst []byte, u uint64, exp int8, trim bool) []byte {
	if exp >= 0 {
		dst = strconv.AppendUint(dst, u, 10)
		if u != 0 {
			for range exp {
				dst = append(dst, '0')
			}
		}
		return dst
	}
	var buf [20]byte
	digits := strconv.AppendUint(buf[:0], u, 10)
	lead := -int(exp) - len(digits) // zeros between the point and the first digit
	if lead < 0 {
		dst = append(dst, digits[:-lead]...)
		digits, lead = digits[-lead:], 0
	} else {
		dst = append(dst, '0')
	}
	if trim {
		for len(digits) > 0 && digits[len(digits)-1] == '0' {
			digits = digits[:len(digits)-1]
		}
		if len(digits) == 0 {
			return dst
		}
	}
	dst = append(dst, '.')
	for range lead {
		dst = append(dst, '0')
	}
	return append(dst, digits...)
}

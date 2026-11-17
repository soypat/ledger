// Package fixed does decimal fixed-point arithmetic on unsigned integers: ASCII text
// parsed and printed at a fixed exponent, so q at exponent -2 counts hundredths;
// rounding by a [Round] the caller names; and exact 128-bit multiply-divide.
package fixed

import (
	"math"
	"math/bits"
	"strconv"
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
// "15", 2 for "1.5e3". It reports the same syntax errors as [Parse], ignoring sign.
func Exp(b []byte) (int, Err) {
	num, err := scan(b)
	return num.sci - num.nfrac, err
}

// split reads ASCII decimal b as the magnitude q with q·10^exp equal to |b|
// truncated, and the tail cut off below it: exp -2 counts hundredths. ErrRange if q
// passes 2^64-1.
func split(b []byte, exp int8) (q uint64, neg bool, t tail, err Err) {
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
		t = tailAbove
	case first == 5:
		t = tailHalf
	case first != 0 || sticky:
		t = tailBelow
	}
	return q, num.neg, t, 0
}

// Parse reads unsigned ASCII decimal b, such as "1234.56" or "1.5e3", as the q with
// q·10^exp equal to b, rounding digits below exp by mode: exp -2 counts hundredths.
// ErrSyntax on anything but an optional sign, digits with at most one point, and an
// optional e or E exponent; ErrRange if negative, except zero, or past 2^64-1;
// ErrInexact if mode is RoundExact and digits below exp are nonzero. It panics on an
// invalid mode.
func Parse(b []byte, exp int8, mode Round) (uint64, Err) {
	CheckRound(mode)
	q, neg, t, err := split(b, exp)
	switch {
	case err != 0:
		return 0, err
	case neg && (q != 0 || t != tailZero):
		return 0, ErrRange
	}
	return roundUp(q, t, mode)
}

// Append appends u·10^exp in decimal to dst: 123456 at exp -2 is "1234.56". With trim,
// trailing fractional zeros and a bare point are dropped ("1234.5", "12"); without, the
// fraction always has -exp digits ("1234.50", "12.00"). For display, a nonzero point
// replaces '.' and a nonzero group goes between every three integer digits: with group
// '.' and point ',' 123456 at exp -2 is "1.234,56". With both zero, [Parse] at exp
// inverts it.
func Append(dst []byte, u uint64, exp int8, trim bool, group, point byte) []byte {
	start := len(dst)
	dst = appendPlain(dst, u, exp, trim)
	if group == 0 && point == 0 {
		return dst
	}
	return regroup(dst, start, group, point)
}

// regroup rewrites the plain decimal dst[start:] in place for display: point for '.'
// unless 0, and group before every third integer digit counted from the point unless 0.
func regroup(dst []byte, start int, group, point byte) []byte {
	p := start // index of the point, or of the end if there is no fraction
	for p < len(dst) && dst[p] != '.' {
		p++
	}
	if p < len(dst) && point != 0 {
		dst[p] = point
	}
	marks := (p - start - 1) / 3 // one between every three integer digits
	if group == 0 || marks == 0 {
		return dst
	}
	dst = append(dst, make([]byte, marks)...)  // grows dst without allocating a temporary
	copy(dst[p+marks:], dst[p:len(dst)-marks]) // the fraction moves right
	// Integer digits move right from the last one, a mark before every third. w stays
	// ahead of r by the marks still to place, so no digit is overwritten unread.
	for r, w, n := p, p+marks, 0; w > r; n++ {
		if n > 0 && n%3 == 0 {
			w--
			dst[w] = group
		}
		r--
		w--
		dst[w] = dst[r]
	}
	return dst
}

func appendPlain(dst []byte, u uint64, exp int8, trim bool) []byte {
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

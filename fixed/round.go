package fixed

import "math/bits"

// Add all enum-like types to the following go:generate command:
//go:generate stringer -type=Round,Err -linecomment -output stringers.go

// Err is why an operation failed; the zero value is success. Returning one does not
// allocate.
type Err uint8

const (
	_          Err = iota
	ErrSyntax      // invalid syntax
	ErrRange       // value out of range
	ErrInexact     // inexact result
)

func (e Err) Error() string { return e.String() }

// Round names a rounding rule. The zero value is invalid, so callers always choose;
// functions taking a Round panic on an invalid one. Rules act on magnitudes.
type Round uint8

const (
	RoundExact    Round = iota + 1 // exact
	RoundDown                      // down
	RoundUp                        // up
	RoundHalfAway                  // half away
	RoundHalfEven                  // half even

	roundMax = iota + 1 // one past the last Round; untyped so stringer skips it
)

// CheckRound panics if m is not a defined Round.
func CheckRound(m Round) {
	if m == 0 || m >= roundMax {
		panic("fixed: invalid Round")
	}
}

// tail is what was cut off below the unit of a truncated magnitude, against half a unit.
type tail uint8

const (
	tailZero  tail = iota // nothing, or only zeros
	tailBelow             // below half a unit
	tailHalf              // exactly half a unit
	tailAbove             // above half a unit
)

// roundsUp reports whether truncated magnitude q rounds up by mode, given its tail. ok
// is false if mode is RoundExact and the tail is nonzero.
func roundsUp(mode Round, q uint64, t tail) (up, ok bool) {
	if t == tailZero {
		return false, true
	}
	switch mode {
	case RoundExact:
		return false, false
	case RoundDown:
		return false, true
	case RoundUp:
		return true, true
	case RoundHalfAway:
		return t >= tailHalf, true
	case RoundHalfEven:
		return t == tailAbove || t == tailHalf && q&1 == 1, true
	}
	panic("fixed: invalid Round")
}

// roundUp rounds truncated magnitude q by mode, given its tail: ErrInexact if mode is
// RoundExact and the tail is nonzero, ErrRange if rounding up passes 2^64-1.
func roundUp(q uint64, t tail, mode Round) (uint64, Err) {
	up, ok := roundsUp(mode, q, t)
	switch {
	case !ok:
		return 0, ErrInexact
	case up:
		q++
		if q == 0 {
			return 0, ErrRange
		}
	}
	return q, 0
}

// MulDiv returns a·num/den rounded by mode, exact through a 128-bit product. ErrRange
// if the result passes 2^64-1, ErrInexact if mode is RoundExact and the division is
// inexact. It panics on den == 0 or an invalid mode.
func MulDiv(a, num, den uint64, mode Round) (uint64, Err) {
	CheckRound(mode)
	if den == 0 {
		panic("fixed: MulDiv by zero")
	}
	hi, lo := bits.Mul64(a, num)
	if hi >= den {
		return 0, ErrRange // the quotient needs more than 64 bits
	}
	q, r := bits.Div64(hi, lo, den)
	t := tailZero
	switch {
	case r == 0:
	case r < den-r: // r < den, so den-r cannot wrap
		t = tailBelow
	case r == den-r:
		t = tailHalf
	default:
		t = tailAbove
	}
	return roundUp(q, t, mode)
}

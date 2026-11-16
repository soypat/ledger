package money

import "math/bits"

// Allocate appends to dst parts of total proportional to weights that sum exactly to
// total. Each part is total·w/Σw rounded down, and the k units left over go one each to
// the parts with the largest remainders, earlier parts winning ties: the largest
// remainder method. False, dst unchanged, if the weights sum to zero or past 2^64-1.
//
// It uses no memory beyond dst: remainders are recomputed rather than stored, so it
// takes O(n·k) time for n weights, with k < n.
func Allocate(dst []uint64, total uint64, weights []uint64) ([]uint64, bool) {
	var sum, carry uint64
	for _, w := range weights {
		sum, carry = bits.Add64(sum, w, 0)
		if carry != 0 {
			return dst, false
		}
	}
	if sum == 0 {
		return dst, false
	}
	start := len(dst)
	var given uint64
	for _, w := range weights {
		q, _ := share(total, w, sum)
		dst = append(dst, q)
		given += q // the floors sum to at most total
	}
	parts := dst[start:]
	for left := total - given; left > 0; left-- {
		// The largest remainder among parts still at their floor, the earliest on a tie.
		// One with a nonzero remainder always remains: those raised so far hold under one
		// unit each of the left·sum still unassigned.
		at, best := -1, uint64(0)
		for i, w := range weights {
			q, r := share(total, w, sum)
			if parts[i] == q && (at < 0 || r > best) {
				at, best = i, r
			}
		}
		parts[at]++
	}
	return dst, true
}

// share returns total·w/sum and its remainder. w ≤ sum, so the quotient fits 64 bits.
func share(total, w, sum uint64) (q, r uint64) {
	hi, lo := bits.Mul64(total, w)
	return bits.Div64(hi, lo, sum)
}

package money

import "github.com/soypat/ledger/internal/fixed"

// Grouping writes decimal amounts for display: Group between every three integer digits
// and Point before the fraction. es-AR is {'.', ','} and en-US is {',', '.'}. A zero
// Group writes no group marks and a zero Point writes '.', so the zero Grouping writes
// as [AppendAmount] does.
type Grouping struct {
	Group, Point byte
}

// AppendUint appends v·10^exp to dst with every fractional digit, as [AppendAmount] but
// grouped: 123456 at exp -2 is "1.234,56" for es-AR.
func (g Grouping) AppendUint(dst []byte, v uint64, exp int8) []byte {
	start := len(dst)
	dst = fixed.Append(dst, v, exp, false)
	point := start // index of the point, or of the end if there is no fraction
	for point < len(dst) && dst[point] != '.' {
		point++
	}
	if point < len(dst) && g.Point != 0 {
		dst[point] = g.Point
	}
	marks := (point - start - 1) / 3 // one between every three integer digits
	if g.Group == 0 || marks == 0 {
		return dst
	}
	dst = append(dst, make([]byte, marks)...)
	copy(dst[point+marks:], dst[point:len(dst)-marks]) // the fraction moves right
	// Integer digits move right from the last one, a mark before every third. w stays
	// ahead of r by the marks still to place, so no digit is overwritten unread.
	for r, w, n := point, point+marks, 0; w > r; n++ {
		if n > 0 && n%3 == 0 {
			w--
			dst[w] = g.Group
		}
		r--
		w--
		dst[w] = dst[r]
	}
	return dst
}

// AppendInt is AppendUint for a signed v, such as a ledger balance: "-1.234,56".
func (g Grouping) AppendInt(dst []byte, v int64, exp int8) []byte {
	u := uint64(v)
	if v < 0 {
		dst = append(dst, '-')
		u = -u // the magnitude, also of the smallest int64
	}
	return g.AppendUint(dst, u, exp)
}

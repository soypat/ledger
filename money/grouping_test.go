package money

import (
	"bytes"
	"math"
	"math/rand/v2"
	"strconv"
	"testing"

	"github.com/soypat/ledger/internal/fixed"
)

// TestGrouping checks AppendUint and AppendInt against what they promise: the digits
// of AppendAmount, with Point in place of its point and Group between every three
// integer digits. Each case is derived from the plain text, never pinned.
func TestGrouping(t *testing.T) {
	groupings := []Grouping{{}, {Group: '.', Point: ','}, {Group: ',', Point: '.'}, {Group: ' '}, {Point: ','}}
	exps := []int8{0, -1, -2, -3, -4, -9, -19, -20, -25, 2}
	values := []uint64{0, 1, 9, 10, 99, 100, 999, 1000, 1001, 999_999, 1_000_000, 123_456, 1234_567_890,
		1<<32 - 1, 1 << 32, 1<<63 - 1, 1 << 63, math.MaxUint64 - 1, math.MaxUint64}
	for _, g := range groupings {
		for _, exp := range exps {
			for _, v := range values {
				testGroupingUint(t, g, v, exp)
			}
			for _, v := range []int64{math.MinInt64, math.MinInt64 + 1, -1_000_000, -1000, -999, -1, 0, 1, 999, 1000, math.MaxInt64} {
				testGroupingInt(t, g, v, exp)
			}
		}
	}
	rng := rand.New(rand.NewPCG(21, 22))
	for range 5000 {
		g := groupings[rng.IntN(len(groupings))]
		exp := int8(-rng.IntN(22))
		testGroupingUint(t, g, rng.Uint64()>>rng.IntN(64), exp)
		testGroupingInt(t, g, int64(rng.Uint64())>>rng.IntN(64), exp)
	}

	dst := make([]byte, 0, 64)
	g := Grouping{Group: '.', Point: ','}
	if n := testing.AllocsPerRun(100, func() { g.AppendUint(dst[:0], math.MaxUint64, -2) }); n != 0 {
		t.Errorf("AppendUint allocates %v times", n)
	}
	if n := testing.AllocsPerRun(100, func() { g.AppendInt(dst[:0], math.MinInt64, -2) }); n != 0 {
		t.Errorf("AppendInt allocates %v times", n)
	}
}

func testGroupingUint(t *testing.T, g Grouping, v uint64, exp int8) {
	t.Helper()
	prefix := []byte("x=")
	got := g.AppendUint(prefix, v, exp)
	if !bytes.HasPrefix(got, prefix) {
		t.Fatalf("%+v.AppendUint(%d, %d) = %q: dst prefix lost", g, v, exp, got)
	}
	got = got[len(prefix):]
	want := grouped(g, fixed.Append(nil, v, exp, false))
	if !bytes.Equal(got, want) {
		t.Errorf("%+v.AppendUint(%d, %d) = %q; the grouping rule gives %q", g, v, exp, got, want)
	}
	// Dropping the marks and restoring the point reads back as the value.
	plain := ungrouped(g, got)
	if back, err := fixed.Parse(plain, exp, RoundExact); err != 0 || back != v {
		t.Errorf("%+v.AppendUint(%d, %d) = %q: ungrouped %q parses to %d, %v", g, v, exp, got, plain, back, err)
	}
}

func testGroupingInt(t *testing.T, g Grouping, v int64, exp int8) {
	t.Helper()
	got := g.AppendInt(nil, v, exp)
	mag := uint64(v)
	want := []byte{}
	if v < 0 {
		mag, want = -mag, []byte{'-'}
	}
	want = g.AppendUint(want, mag, exp)
	if !bytes.Equal(got, want) {
		t.Errorf("%+v.AppendInt(%d, %d) = %q; want sign then AppendUint of the magnitude, %q", g, v, exp, got, want)
	}
	if v >= 0 && len(got) > 0 && got[0] == '-' {
		t.Errorf("%+v.AppendInt(%d, %d) = %q: sign on a non-negative value", g, v, exp, got)
	}
}

// grouped applies the Grouping rule to plain decimal text: Point for '.', and Group
// before every third integer digit counted from the point, never before the first.
func grouped(g Grouping, plain []byte) []byte {
	intPart, frac, hasPoint := bytes.Cut(plain, []byte{'.'})
	var out []byte
	for i, c := range intPart {
		if g.Group != 0 && i > 0 && (len(intPart)-i)%3 == 0 {
			out = append(out, g.Group)
		}
		out = append(out, c)
	}
	if hasPoint {
		point := g.Point
		if point == 0 {
			point = '.'
		}
		out = append(append(out, point), frac...)
	}
	return out
}

// ungrouped inverts grouped: Group marks dropped, Point back to '.'.
func ungrouped(g Grouping, text []byte) []byte {
	var out []byte
	for _, c := range text {
		switch {
		case g.Group != 0 && c == g.Group:
		case g.Point != 0 && c == g.Point:
			out = append(out, '.')
		default:
			out = append(out, c)
		}
	}
	return out
}

// TestGroupingIntegerDigits checks the marks alone: at exp 0 the integer part is the
// decimal digits of v, so the number of marks follows from the number of digits.
func TestGroupingIntegerDigits(t *testing.T) {
	g := Grouping{Group: '\''}
	for _, v := range []uint64{0, 1, 999, 1000, 999_999, 1_000_000, math.MaxUint64} {
		got := g.AppendUint(nil, v, 0)
		digits := len(strconv.FormatUint(v, 10))
		if marks := bytes.Count(got, []byte{'\''}); marks != (digits-1)/3 || len(got) != digits+marks {
			t.Errorf("AppendUint(%d, 0) = %q: %d marks for %d digits", v, got, marks, digits)
		}
	}
}

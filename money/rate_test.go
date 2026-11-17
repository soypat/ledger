package money

import (
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/soypat/ledger/fixed"
)

func TestRate(t *testing.T) {
	parse := []struct {
		s    string
		want Rate
		err  error
	}{
		{"21%", 210_000, nil},
		{"10.5%", 105_000, nil},
		{"0.21", 210_000, nil},
		{"130%", 1_300_000, nil},
		{"0.0001%", 1, nil},
		{"0%", 0, nil},
		{"0.00001%", 0, fixed.ErrInexact},
		{"0.0000001", 0, fixed.ErrInexact},
		{"-1%", 0, fixed.ErrRange},
		{"429497%", 0, fixed.ErrRange}, // 4_294_970_000 ppm passes a uint32
		{"%", 0, fixed.ErrSyntax},
		{"21%%", 0, fixed.ErrSyntax},
		{"", 0, fixed.ErrSyntax},
	}
	for _, c := range parse {
		got, err := ParseRate([]byte(c.s))
		if got != c.want || err != c.err {
			t.Errorf("ParseRate(%q) = %d, %v; want %d, %v", c.s, got, err, c.want, c.err)
		}
	}
	for _, r := range []Rate{0, 1, 210_000, 105_000, ppm_whole, 1_300_000, 4_294_967_295} {
		s := r.String()
		if got, err := ParseRate([]byte(s)); got != r || err != nil {
			t.Errorf("ParseRate(%q) = %d, %v; want %d", s, got, err, r)
		}
	}
	if got := Rate(210_000).String(); got != "21%" {
		t.Errorf("String = %q; want 21%%", got)
	}

	of := []struct {
		r      Rate
		amount uint64
		mode   Round
		want   uint64
		ok     bool
	}{
		{210_000, 100_000, RoundExact, 21_000, true},
		{210_000, 12_345, RoundHalfAway, 2_592, true}, // 2592.45
		{210_000, 12_345, RoundUp, 2_593, true},
		{210_000, 12_345, RoundExact, 0, false},
		{1_300_000, 1_000, RoundExact, 1_300, true},
		{ppm_whole, 1<<64 - 1, RoundExact, 1<<64 - 1, true},
		{ppm_whole + 1, 1<<64 - 1, RoundDown, 0, false},
	}
	for _, c := range of {
		got, ok := c.r.Of(c.amount, c.mode)
		if got != c.want || ok != c.ok {
			t.Errorf("Rate(%d).Of(%d, %v) = %d, %v; want %d, %v", c.r, c.amount, c.mode, got, ok, c.want, c.ok)
		}
	}
}

func TestMonth(t *testing.T) {
	m := MonthOf(time.Date(2026, time.November, 15, 13, 0, 0, 0, time.UTC))
	if m != 2026*12+10 {
		t.Fatalf("MonthOf(2026-11-15) = %d", m)
	}
	if got, want := m.Time(), time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("Time() = %v; want %v", got, want)
	}
	if got := (m + 2).Time(); got.Year() != 2027 || got.Month() != time.January {
		t.Errorf("two months after November 2026 = %v", got)
	}
	for _, mo := range []Month{-25, -13, -12, -1, 0, 1, 11, 12, 24299, 24300} {
		if got := MonthOf(mo.Time()); got != mo {
			t.Errorf("MonthOf(Month(%d).Time() = %v) = %d", mo, mo.Time(), got)
		}
	}
}

func TestIndex(t *testing.T) {
	jan := MonthOf(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))
	x := Index{Start: jan, Values: []uint64{100, 0, 150}}
	at := []struct {
		m    Month
		want uint64
		ok   bool
	}{
		{jan, 100, true},
		{jan + 1, 0, false}, // zero: not yet published
		{jan + 2, 150, true},
		{jan - 1, 0, false},
		{jan + 3, 0, false},
	}
	for _, c := range at {
		if got, ok := x.At(c.m); got != c.want || ok != c.ok {
			t.Errorf("At(%d) = %d, %v; want %d, %v", c.m-jan, got, ok, c.want, c.ok)
		}
	}
	adjust := []struct {
		amount   uint64
		from, to Month
		mode     Round
		want     uint64
		ok       bool
	}{
		{1000, jan, jan + 2, RoundExact, 1500, true},
		{1000, jan + 2, jan, RoundHalfAway, 667, true}, // 666.67
		{1000, jan + 2, jan, RoundDown, 666, true},
		{1000, jan + 2, jan, RoundExact, 0, false},
		{1000, jan, jan + 1, RoundExact, 0, false},
		{1000, jan - 1, jan, RoundExact, 0, false},
	}
	for _, c := range adjust {
		got, ok := x.Adjust(c.amount, c.from, c.to, c.mode)
		if got != c.want || ok != c.ok {
			t.Errorf("Adjust(%d, %d, %d, %v) = %d, %v; want %d, %v", c.amount, c.from-jan, c.to-jan, c.mode, got, ok, c.want, c.ok)
		}
	}
	mustPanic(t, "Adjust mode 0", func() { x.Adjust(1, jan, jan, 0) })
}

// Edges crossed by the Gross, Net and Compound tests, with every Round.
var (
	rateEdges   = []Rate{0, 1, 105_000, 210_000, ppm_whole - 1, ppm_whole, 1_300_000, math.MaxUint32}
	amountEdges = []uint64{0, 1, 2, 99, 100, 12_345, 999_999, 1_000_000, 1<<32 + 1, 1<<63 - 1, 1 << 63, math.MaxUint64 - 1, math.MaxUint64}
	allRounds   = []Round{RoundExact, RoundDown, RoundUp, RoundHalfAway, RoundHalfEven}
)

// rounds reports whether g is num/den rounded by mode, straight from each rule's
// definition and in big integers, so that nothing overflows.
func rounds(g uint64, num, den *big.Int, mode Round) bool {
	// above is g·den − num: how far g lies above the exact quotient, in units of 1/den.
	above := new(big.Int).SetUint64(g)
	above.Mul(above, den).Sub(above, num)
	twice := new(big.Int).Abs(above)
	half := twice.Lsh(twice, 1).Cmp(den) // g's distance from the quotient, against half a unit
	switch mode {
	case RoundExact:
		return above.Sign() == 0
	case RoundDown:
		return above.Sign() <= 0 && above.CmpAbs(den) < 0
	case RoundUp:
		return above.Sign() >= 0 && above.CmpAbs(den) < 0
	case RoundHalfAway:
		return half < 0 || half == 0 && above.Sign() > 0
	case RoundHalfEven:
		return half < 0 || half == 0 && g%2 == 0
	}
	panic("invalid Round")
}

// rounded reports what is wrong with got, ok as num/den rounded by mode; empty if
// nothing. Either got follows the rule, or ok is false, got is zero and no uint64 does.
func rounded(got uint64, ok bool, num, den *big.Int, mode Round) string {
	if ok {
		if !rounds(got, num, den, mode) {
			return fmt.Sprintf("not %v/%v rounded %v", num, den, mode)
		}
		return ""
	}
	if got != 0 {
		return "failed, yet returned a value"
	}
	// Only the floor of the quotient and the integer above it can follow a rule.
	cand := new(big.Int).Quo(num, den)
	for range 2 {
		if cand.IsUint64() && rounds(cand.Uint64(), num, den, mode) {
			return fmt.Sprintf("failed, though %v is %v/%v rounded %v", cand, num, den, mode)
		}
		cand.Add(cand, big.NewInt(1))
	}
	return ""
}

func TestRateGrossNet(t *testing.T) {
	for _, r := range rateEdges {
		for _, amount := range amountEdges {
			testGrossNet(t, r, amount)
		}
	}
	rng := rand.New(rand.NewPCG(11, 12))
	for range 20000 { // rates and amounts of every magnitude
		testGrossNet(t, Rate(rng.Uint32()>>rng.IntN(32)), rng.Uint64()>>rng.IntN(64))
	}
	mustPanic(t, "Gross mode 0", func() { Rate(1).Gross(1, 0) })
	mustPanic(t, "Net mode 0", func() { Rate(1).Net(1, 0) })
}

// testGrossNet checks Gross and Net of amount at r under every Round: each result
// against the rounding rule's definition, then what the two promise of each other.
func testGrossNet(t *testing.T, r Rate, amount uint64) {
	t.Helper()
	whole := big.NewInt(int64(ppm_whole))
	plus := new(big.Int).Add(whole, big.NewInt(int64(r))) // 1+r
	a := new(big.Int).SetUint64(amount)
	grossNum, netNum := new(big.Int).Mul(a, plus), new(big.Int).Mul(a, whole)
	for _, mode := range allRounds {
		g, gok := r.Gross(amount, mode)
		if bad := rounded(g, gok, grossNum, whole, mode); bad != "" {
			t.Errorf("Rate(%d).Gross(%d, %v) = %d, %v: %s", r, amount, mode, g, gok, bad)
		}
		n, nok := r.Net(amount, mode)
		if bad := rounded(n, nok, netNum, plus, mode); bad != "" {
			t.Errorf("Rate(%d).Net(%d, %v) = %d, %v: %s", r, amount, mode, n, nok, bad)
		}
		switch {
		case gok && g < amount:
			t.Errorf("Rate(%d).Gross(%d, %v) = %d: below the net amount", r, amount, mode, g)
		case n > amount:
			t.Errorf("Rate(%d).Net(%d, %v) = %d: above the gross amount", r, amount, mode, n)
		case !nok && mode != RoundExact:
			t.Errorf("Rate(%d).Net(%d, %v) failed, though it never passes gross", r, amount, mode)
		case r == 0 && (!gok || !nok || g != amount || n != amount):
			t.Errorf("Rate(0) changed %d under %v: Gross %d, %v; Net %d, %v", amount, mode, g, gok, n, nok)
		}
		if mode != RoundExact {
			continue
		}
		// What one gives exactly, the other takes back exactly.
		if back, ok := r.Net(g, RoundExact); gok && (!ok || back != amount) {
			t.Errorf("Rate(%d): Gross(%d) = %d exactly, but Net of it = %d, %v", r, amount, g, back, ok)
		}
		if back, ok := r.Gross(n, RoundExact); nok && (!ok || back != amount) {
			t.Errorf("Rate(%d): Net(%d) = %d exactly, but Gross of it = %d, %v", r, amount, n, back, ok)
		}
	}
}

func TestRateCompound(t *testing.T) {
	for _, r := range rateEdges {
		for _, amount := range amountEdges {
			for _, mode := range allRounds {
				testCompound(t, r, amount, mode)
			}
		}
	}
	rng := rand.New(rand.NewPCG(13, 14))
	for range 500 {
		r, amount := Rate(rng.Uint32()>>rng.IntN(32)), rng.Uint64()>>rng.IntN(64)
		mode := allRounds[rng.IntN(len(allRounds))]
		testCompound(t, r, amount, mode)

		// Periods add up: i then j of them is i+j, in either direction.
		i, j := rng.IntN(12), rng.IntN(12)
		if rng.IntN(2) == 0 {
			i, j = -i, -j
		}
		first, ok1 := r.Compound(amount, i, mode)
		then, ok2 := r.Compound(first, j, mode)
		whole, ok := r.Compound(amount, i+j, mode)
		if ok1 && (then != whole || ok2 != ok) {
			t.Errorf("Rate(%d).Compound(%d, %v): %d then %d periods = %d, %v; %d at once = %d, %v",
				r, amount, mode, i, j, then, ok2, i+j, whole, ok)
		}
	}
	mustPanic(t, "Compound mode 0", func() { Rate(1).Compound(1, 0, 0) })
	if n := testing.AllocsPerRun(100, func() { Rate(210_000).Compound(12_345, 24, RoundHalfEven) }); n != 0 {
		t.Errorf("Compound allocates %v times", n)
	}
}

// testCompound checks Compound of amount at r against its definition, in both
// directions: no periods is the amount itself, one period more is Gross of the amount
// so far (Net, when discounting), and a failure stays one. Then far periods: once the
// amount fails or stops moving, every later period agrees.
func testCompound(t *testing.T, r Rate, amount uint64, mode Round) {
	t.Helper()
	const near, settleWithin = 24, 2000
	if got, ok := r.Compound(amount, 0, mode); got != amount || !ok {
		t.Errorf("Rate(%d).Compound(%d, 0, %v) = %d, %v; no periods must change nothing", r, amount, mode, got, ok)
		return
	}
	for _, dir := range []int{1, -1} {
		step, far := Rate.Gross, math.MaxInt
		if dir < 0 {
			step, far = Rate.Net, math.MinInt
		}
		cur, ok, settled := amount, true, false
		for k := 1; k <= settleWithin && !settled; k++ {
			next, nextOK := step(r, cur, mode)
			if k <= near {
				if got, gotOK := r.Compound(amount, k*dir, mode); got != next || gotOK != nextOK {
					t.Errorf("Rate(%d).Compound(%d, %d, %v) = %d, %v; a period on from %d is %d, %v",
						r, amount, k*dir, mode, got, gotOK, cur, next, nextOK)
					return
				}
			}
			settled = !nextOK || next == cur
			cur, ok = next, nextOK
		}
		if !settled {
			continue // still moving: far periods would take too long to follow
		}
		for _, n := range []int{dir * (settleWithin + 1), far} {
			if got, gotOK := r.Compound(amount, n, mode); got != cur || gotOK != ok {
				t.Errorf("Rate(%d).Compound(%d, %d, %v) = %d, %v; it settled at %d, %v",
					r, amount, n, mode, got, gotOK, cur, ok)
			}
		}
	}
}

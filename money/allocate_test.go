package money

import (
	"math"
	"math/bits"
	"math/rand/v2"
	"slices"
	"testing"
)

func TestAllocate(t *testing.T) {
	cases := []struct {
		total   uint64
		weights []uint64
		want    []uint64
	}{
		{1_000_000, []uint64{1, 1, 1}, []uint64{333334, 333333, 333333}},
		{100, []uint64{1, 1, 1}, []uint64{34, 33, 33}},
		{10, []uint64{1, 2, 3, 4}, []uint64{1, 2, 3, 4}},
		{10, []uint64{3, 3, 3}, []uint64{4, 3, 3}},
		{100, []uint64{1, 2}, []uint64{33, 67}}, // the larger remainder wins over the earlier part
		{7, []uint64{0, 1, 1}, []uint64{0, 4, 3}},
		{0, []uint64{5, 5}, []uint64{0, 0}},
		{5, []uint64{7}, []uint64{5}},
		{math.MaxUint64, []uint64{1 << 63, 1<<63 - 1}, []uint64{1 << 63, 1<<63 - 1}},
	}
	for _, c := range cases {
		got, ok := Allocate(nil, c.total, c.weights)
		if !ok || !slices.Equal(got, c.want) {
			t.Errorf("Allocate(%d, %v) = %v, %v; want %v", c.total, c.weights, got, ok, c.want)
		}
	}

	dst := []uint64{42}
	if got, ok := Allocate(dst, 3, []uint64{1, 1}); !ok || !slices.Equal(got, []uint64{42, 2, 1}) {
		t.Errorf("Allocate onto dst = %v, %v; want [42 2 1]", got, ok)
	}
	for _, w := range [][]uint64{nil, {0, 0}, {math.MaxUint64, 1}} {
		if got, ok := Allocate(dst, 10, w); ok || !slices.Equal(got, dst) {
			t.Errorf("Allocate(10, %v) = %v, %v; want dst unchanged, false", w, got, ok)
		}
	}
}

// TestAllocateRandom checks the defining properties: the parts sum to total, each is its
// exact quota rounded down or up, and only parts with larger remainders, or equal and
// earlier, are rounded up before a part left down.
func TestAllocateRandom(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 8))
	var parts, weights []uint64
	for range 5000 {
		weights = weights[:0]
		for range 1 + rng.IntN(12) {
			weights = append(weights, rng.Uint64N(1<<uint(rng.IntN(40))))
		}
		total := rng.Uint64() >> uint(rng.IntN(64))
		var ok bool
		parts, ok = Allocate(parts[:0], total, weights)
		var sum uint64
		for _, w := range weights {
			sum += w
		}
		if sum == 0 {
			if ok {
				t.Fatalf("Allocate(%d, %v) accepted zero weights", total, weights)
			}
			continue
		}
		var got uint64
		for i, p := range parts {
			q, r := share(total, weights[i], sum)
			if p != q && p != q+1 || p == q+1 && r == 0 {
				t.Fatalf("Allocate(%d, %v) = %v: part %d is %d, quota %d rem %d", total, weights, parts, i, p, q, r)
			}
			var carry uint64
			got, carry = bits.Add64(got, p, 0)
			if carry != 0 {
				t.Fatalf("Allocate(%d, %v) = %v: parts overflow", total, weights, parts)
			}
		}
		if got != total {
			t.Fatalf("Allocate(%d, %v) = %v: sums to %d", total, weights, parts, got)
		}
		for i := range parts {
			qi, ri := share(total, weights[i], sum)
			for j := range parts {
				qj, rj := share(total, weights[j], sum)
				up, down := parts[i] == qi+1, parts[j] == qj
				if up && down && (ri < rj || ri == rj && i > j) {
					t.Fatalf("Allocate(%d, %v) = %v: part %d raised over part %d", total, weights, parts, i, j)
				}
			}
		}
	}
}

func TestAllocateAllocs(t *testing.T) {
	weights := []uint64{3, 1, 4, 1, 5, 9, 2, 6}
	dst := make([]uint64, 0, len(weights))
	if n := testing.AllocsPerRun(100, func() { Allocate(dst[:0], 1_000_003, weights) }); n != 0 {
		t.Errorf("Allocate allocates %v times", n)
	}
}

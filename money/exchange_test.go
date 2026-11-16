package money

import (
	"math"
	"sync"
	"testing"

	"github.com/soypat/ledger/internal/fixed"
)

func TestPrice(t *testing.T) {
	cases := []struct {
		s    string
		want Price
		err  error
	}{
		{"1185.50", Price{118550, -2}, nil},
		{"1185.5", Price{11855, -1}, nil},
		{"15", Price{15, 0}, nil},
		{"1.5e3", Price{1500, 0}, nil},
		{"1e-3", Price{1, -3}, nil},
		{"0.00084388", Price{84388, -8}, nil},
		{"0", Price{0, 0}, nil},
		{"18446744073709551615", Price{math.MaxUint64, 0}, nil},
		{"18446744073709551616", Price{}, fixed.ErrRange},
		{"1e-200", Price{}, fixed.ErrRange}, // Exp past an int8
		{"1e99999", Price{}, fixed.ErrRange},
		{"-1", Price{}, fixed.ErrRange},
		{"x", Price{}, fixed.ErrSyntax},
	}
	for _, c := range cases {
		got, err := ParsePrice([]byte(c.s))
		if got != c.want || err != c.err {
			t.Errorf("ParsePrice(%q) = %v, %v; want %v, %v", c.s, got, err, c.want, c.err)
		}
	}
	for _, s := range []string{"1185.50", "0.00084388", "15", "0.001", "0"} {
		p, _ := ParsePrice([]byte(s))
		if got := p.String(); got != s {
			t.Errorf("ParsePrice(%q).String() = %q", s, got)
		}
	}
}

func TestQuote(t *testing.T) {
	quote := func(from, to cur, s string) Quote[cur] {
		t.Helper()
		p, err := ParsePrice([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
		q, err := NewQuote(from, to, p)
		if err != nil {
			t.Fatalf("NewQuote(%v, %v, %v): %v", from.Ticker(), to.Ticker(), p, err)
		}
		return q
	}
	usdars := quote(usd, ars, "1185.50")
	arsusd := quote(ars, usd, "0.00084388")
	jpybhd := quote(jpy, bhd, "0.0025")
	bhdjpy := quote(bhd, jpy, "400")
	jpybhd3 := quote(jpy, bhd, "3") // k > 0: one yen is 3000 fils
	cases := []struct {
		q    Quote[cur]
		v    uint64
		mode Round
		want uint64
		ok   bool
	}{
		{usdars, 100, RoundExact, 118550, true}, // 1 USD
		{usdars, 1, RoundExact, 0, false},       // 1185.5 centavos
		{usdars, 1, RoundHalfEven, 1186, true},
		{usdars, 1, RoundDown, 1185, true},
		{usdars, 3, RoundHalfEven, 3556, true}, // 3556.5: 3556 is even
		{arsusd, 118550, RoundDown, 100, true}, // 100.041974
		{arsusd, 118550, RoundHalfAway, 100, true},
		{arsusd, 118550, RoundUp, 101, true},
		{arsusd, 118550, RoundExact, 0, false},
		{jpybhd, 1000, RoundExact, 2500, true}, // 2.500 BHD
		{bhdjpy, 2500, RoundExact, 1000, true},
		{bhdjpy, 1, RoundHalfAway, 0, true}, // 0.4 yen
		{bhdjpy, 1, RoundUp, 1, true},
		{jpybhd3, 2, RoundExact, 6000, true},
		{usdars, math.MaxUint64, RoundDown, 0, false},
	}
	for _, c := range cases {
		got, ok := c.q.Convert(c.v, c.mode)
		if got != c.want || ok != c.ok {
			t.Errorf("%v→%v at %v: Convert(%d, %v) = %d, %v; want %d, %v",
				c.q.From.Ticker(), c.q.To.Ticker(), c.q.Price, c.v, c.mode, got, ok, c.want, c.ok)
		}
	}
	for _, p := range []Price{{0, 0}, {1, 20}, {1 << 62, 2}, {1, -30}} {
		if _, err := NewQuote(usd, ars, p); err != fixed.ErrRange {
			t.Errorf("NewQuote(USD, ARS, %v) err = %v; want fixed.ErrRange", p, err)
		}
	}
	mustPanic(t, "Convert of the zero Quote", func() { Quote[cur]{}.Convert(1, RoundDown) })
	mustPanic(t, "Convert mode 0", func() { usdars.Convert(1, 0) })
}

func TestExchange(t *testing.T) {
	mk := func(from, to cur, p Price) Quote[cur] {
		q, err := NewQuote(from, to, p)
		if err != nil {
			t.Fatal(err)
		}
		return q
	}
	var r Exchange[cur]
	if err := r.Set(mk(usd, ars, Price{1000, 0})); err != errCapacity {
		t.Fatalf("Set on the zero Exchange err = %v; want errCapacity", err)
	}
	r.Configure(make([]Quote[cur], 0, 2))
	steps := []struct {
		q   Quote[cur]
		err error
	}{
		{mk(usd, ars, Price{1000, 0}), nil},
		{mk(ars, usd, Price{1, -3}), nil},
		{mk(usd, jpy, Price{150, 0}), errCapacity},
		{mk(usd, ars, Price{1200, 0}), nil}, // replaces, though full
	}
	for i, s := range steps {
		if err := r.Set(s.q); err != s.err {
			t.Errorf("step %d: Set err = %v; want %v", i, err, s.err)
		}
	}
	if q, ok := r.Quote(usd, ars); !ok || q.Price != (Price{1200, 0}) {
		t.Errorf("Quote(USD, ARS) = %v, %v; want the replacement", q.Price, ok)
	}
	if _, ok := r.Quote(usd, jpy); ok {
		t.Error("Quote(USD, JPY) found a quote never stored")
	}
	converts := []struct {
		from, to cur
		v        uint64
		mode     Round
		want     uint64
		err      error
	}{
		{usd, ars, 100, RoundExact, 120000, nil},
		{ars, usd, 120000, RoundExact, 120, nil},
		{ars, usd, 1, RoundExact, 0, fixed.ErrInexact},
		{usd, ars, math.MaxUint64, RoundDown, 0, fixed.ErrRange},
		{usd, usd, 7, RoundExact, 7, nil},
		{jpy, jpy, 7, RoundExact, 7, nil},
		{usd, jpy, 1, RoundExact, 0, errNoQuote},
	}
	for _, c := range converts {
		got, err := r.Convert(c.from, c.to, c.v, c.mode)
		if got != c.want || err != c.err {
			t.Errorf("Convert(%v, %v, %d, %v) = %d, %v; want %d, %v",
				c.from.Ticker(), c.to.Ticker(), c.v, c.mode, got, err, c.want, c.err)
		}
	}
	mustPanic(t, "Set of the zero Quote", func() { r.Set(Quote[cur]{}) })
	mustPanic(t, "Convert mode 0", func() { r.Convert(usd, usd, 1, 0) })

	q := mk(usd, ars, Price{1300, 0})
	if n := testing.AllocsPerRun(100, func() { r.Set(q) }); n != 0 {
		t.Errorf("Set allocates %v times", n)
	}
	if n := testing.AllocsPerRun(100, func() { r.Convert(usd, ars, 12345, RoundHalfEven) }); n != 0 {
		t.Errorf("Convert allocates %v times", n)
	}

	r.Reset()
	if _, ok := r.Quote(usd, ars); ok || cap(r.quotes) != 2 {
		t.Errorf("after Reset: quote found %v, capacity %d; want none, 2", ok, cap(r.quotes))
	}
	r.Set(q)
	r.Configure(nil)
	if _, ok := r.Quote(usd, ars); ok || cap(r.quotes) != 2 {
		t.Errorf("after Configure(nil): quote found %v, capacity %d; want none, 2", ok, cap(r.quotes))
	}
}

// TestExchangeRace races writers against readers: under -race any unlocked access fails,
// and every conversion must come from one whole quote. Quote i is 1000+i ARS per USD
// written with i%3 extra zeros, so its num and den carry a factor 10^(i%3) that cancels:
// a num and den from different quotes give a value outside 1000 through 1049.
func TestExchangeRace(t *testing.T) {
	const nquotes = 50
	var quotes [nquotes]Quote[cur]
	for i := range quotes {
		zeros := i % 3
		v := uint64(1000+i) * [3]uint64{1, 10, 100}[zeros]
		q, err := NewQuote(usd, ars, Price{V: v, Exp: int8(-zeros)})
		if err != nil {
			t.Fatal(err)
		}
		quotes[i] = q
	}
	var x Exchange[cur]
	x.Configure(make([]Quote[cur], 0, 1))
	x.Set(quotes[0])
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			for i := range 2000 {
				if err := x.Set(quotes[i%nquotes]); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	for range 4 {
		wg.Go(func() {
			for range 2000 {
				q, ok := x.Quote(usd, ars)
				if !ok {
					t.Error("Quote(USD, ARS) missing")
					return
				}
				got, err := x.Convert(usd, ars, 1, RoundExact) // 1 cent is 1000+i centavos
				if err != nil {
					t.Error(err)
					return
				}
				if !validConvert(quotes[:], q, got) {
					t.Errorf("Convert = %d with quote %v", got, q.Price)
					return
				}
			}
		})
	}
	wg.Wait()
	x.Reset()
	if _, err := x.Convert(usd, ars, 100, RoundDown); err != errNoQuote {
		t.Errorf("after Reset Convert err = %v; want errNoQuote", err)
	}
}

// validConvert reports whether q is one of quotes and got is 1 cent at some quote.
func validConvert(quotes []Quote[cur], q Quote[cur], got uint64) bool {
	found := false
	for _, s := range quotes {
		found = found || s == q
	}
	return found && got >= 1000 && got < 1000+uint64(len(quotes))
}

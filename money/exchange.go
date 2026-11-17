package money

import (
	"math/bits"
	"sync"

	"github.com/soypat/ledger/fixed"
)

// Quote converts amounts of From into amounts of To at the price [Quote.Rate] reports.
// Make one with [NewQuote], and one converting the other way with [Quote.Inverse]; the
// zero Quote is invalid. Its fields are read-only, since num and den are
// derived from them.
type Quote[C Currency] struct {
	from, to C
	price    Decimal
	inverse  bool   // price is From per To: q is NewQuote(to, from, price).Inverse()
	num, den uint64 // minor units of To per minor unit of From
}

// NewQuote returns the quote converting from into to at p, major units of to per
// major unit of from. An error if p is zero, or with the currencies' exponents it
// passes what a ratio of uint64 holds.
func NewQuote[C Currency](from, to C, p Decimal) (Quote[C], error) {
	if p.V == 0 {
		return Quote[C]{}, fixed.ErrRange
	}
	q := Quote[C]{from: from, to: to, price: p, num: p.V, den: 1}
	// v minor From is v·10^from.Exp major From, worth v·p.v·10^(from.Exp+p.exp) major To,
	// which is v·p.v·10^k minor To.
	k := int(from.Exp()) + int(p.Exp) - int(to.Exp())
	if k >= 0 {
		m, ok := fixed.Pow10(k)
		hi, lo := bits.Mul64(p.V, m)
		if !ok || hi != 0 {
			return Quote[C]{}, fixed.ErrRange
		}
		q.num = lo
	} else {
		d, ok := fixed.Pow10(-k)
		if !ok {
			return Quote[C]{}, fixed.ErrRange
		}
		q.den = d
	}
	return q, nil
}

// From returns the currency q converts from.
func (q Quote[C]) From() C { return q.from }

// To returns the currency q converts into.
func (q Quote[C]) To() C { return q.to }

// Rate returns q's original quote before inversion if applied.
func (q Quote[C]) Rate() (base, counter C, price Decimal) {
	if q.inverse {
		return q.to, q.from, q.price
	}
	return q.from, q.to, q.price
}

// Inverse returns exact quote converting To into From at the same price. The inverse of the inverse is original Quote.
func (q Quote[C]) Inverse() Quote[C] {
	q.mustValid()
	q.from, q.to, q.num, q.den, q.inverse = q.to, q.from, q.den, q.num, !q.inverse
	return q
}

// Ratio returns q as minor units of To per minor unit of From, num/den, for callers
// folding the conversion into a larger multiply-divide that rounds once. It panics on a
// Quote not made by NewQuote.
func (q Quote[C]) Ratio() (num, den uint64) {
	q.mustValid()
	return q.num, q.den
}

// Convert returns a in To, rounded by mode, exact through a 128-bit product; invalid if
// a is invalid, the result passes 2^64-1, or mode is RoundExact and it is inexact. It
// panics if a is not in From, on an invalid mode, or on a Quote not made by NewQuote.
func (q Quote[C]) Convert(a Amount[C], mode Round) Amount[C] {
	v, err := q.convert(a, mode)
	return Amount[C]{v: v, c: q.to, ok: a.ok && err == 0}
}

func (q *Quote[C]) convert(a Amount[C], mode Round) (uint64, fixed.Err) {
	q.mustValid()
	if a.c != q.from {
		panic("money: Quote.Convert of " + a.c.Ticker() + " at a quote from " + q.from.Ticker())
	}
	return fixed.MulDiv(a.v, q.num, q.den, mode)
}

func (q *Quote[C]) mustValid() {
	if q.den == 0 {
		panic("money: Quote not made by NewQuote")
	}
}

// Exchange converts amounts between currencies at the quotes it holds, each one way,
// since a market's buy and sell quotes for a pair differ. It holds them in memory the
// caller gives [Exchange.Configure]; the zero Exchange holds none.
//
// It is safe for concurrent use, such as a feed calling Set while requests call
// Convert. Updates go first: once Set waits for the lock, readers that arrive after it
// wait too, so they see the new quote. It must not be copied after first use.
type Exchange[C Currency] struct {
	mu     sync.RWMutex // a waiting Lock blocks new RLocks
	quotes []Quote[C]
}

// Configure gives x buf to hold up to cap(buf) quotes, or keeps x's current memory if
// buf is nil, and removes every quote.
func (x *Exchange[C]) Configure(buf []Quote[C]) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if buf == nil {
		buf = x.quotes
	}
	buf = buf[:cap(buf)]
	clear(buf)
	x.quotes = buf[:0]
}

// Reset removes every quote and keeps x's memory.
func (x *Exchange[C]) Reset() { x.Configure(nil) }

// Set adds q, replacing any quote with the same From and To; an error if x is full.
// It panics on a Quote not made by NewQuote.
func (x *Exchange[C]) Set(q Quote[C]) error {
	q.mustValid()
	x.mu.Lock()
	defer x.mu.Unlock()
	if i := x.index(q.from, q.to); i >= 0 {
		x.quotes[i] = q
		return nil
	}
	if len(x.quotes) == cap(x.quotes) {
		return errCapacity
	}
	x.quotes = append(x.quotes, q)
	return nil
}

// index returns the position of the quote from from into to, or -1. Hold mu.
func (x *Exchange[C]) index(from, to C) int {
	for i := range x.quotes {
		if x.quotes[i].from == from && x.quotes[i].to == to {
			return i
		}
	}
	return -1
}

// Quote returns the quote from from into to; false if x has none. Converting several
// amounts with the returned Quote uses one rate for all, even if Set replaces it
// meanwhile.
func (x *Exchange[C]) Quote(from, to C) (Quote[C], bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	i := x.index(from, to)
	if i < 0 {
		return Quote[C]{}, false
	}
	return x.quotes[i], true
}

// Convert returns a in currency to, rounded by mode, or a itself if already in to. An
// error, with an invalid Amount in to, if a is invalid, x has no quote from a's currency
// into to, the result passes 2^64-1, or mode is RoundExact and it is inexact. It panics
// on an invalid mode.
func (x *Exchange[C]) Convert(a Amount[C], to C, mode Round) (Amount[C], error) {
	fixed.CheckRound(mode)
	switch {
	case !a.ok:
		return Amount[C]{c: to}, errInvalidAmount
	case a.c == to:
		return a, nil
	}
	q, ok := x.Quote(a.c, to)
	if !ok {
		return Amount[C]{c: to}, errNoQuote
	}
	v, err := q.convert(a, mode)
	if err != 0 {
		return Amount[C]{c: to}, err
	}
	return FromMinor(to, v), nil
}

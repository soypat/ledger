package money

import (
	"math"
	"math/bits"
	"sync"

	"github.com/soypat/ledger/internal/fixed"
)

// Price is an exact decimal V·10^Exp, as a market quotes an exchange rate: major units
// of one currency per major unit of another, such as 1185.50 ARS per USD.
type Price struct {
	V   uint64
	Exp int8
}

// ParsePrice parses decimal text such as "1185.50" exactly, keeping every digit:
// {118550, -2}. Exp is that of the last digit but at most 0, so "1.5e3" is {1500, 0}.
// An error on syntax as in [ParseAmount], or if negative, past 2^64-1, or Exp would
// pass an int8.
func ParsePrice(b []byte) (Price, error) {
	e, err := fixed.Exp(b)
	switch {
	case err != 0:
		return Price{}, err
	case e < math.MinInt8:
		return Price{}, fixed.ErrRange
	}
	exp := int8(min(e, 0))
	v, err := fixed.Parse(b, exp, RoundExact) // keeps every digit, so it is never inexact
	if err != 0 {
		return Price{}, err
	}
	return Price{V: v, Exp: exp}, nil
}

// StringAppend appends p to dst with all its digits: {118550, -2} is "1185.50".
// [ParsePrice] inverts it.
func (p Price) StringAppend(dst []byte) []byte {
	return fixed.Append(dst, p.V, p.Exp, false)
}

func (p Price) String() string { return string(p.StringAppend(nil)) }

// Quote converts amounts of From into amounts of To at Price, in major units of To per
// major unit of From. Make one with [NewQuote]; the zero Quote is invalid.
type Quote[C Currency] struct {
	From, To C
	Price    Price
	num, den uint64 // minor units of To per minor unit of From
}

// NewQuote returns the quote converting from into to at p, major units of to per
// major unit of from. An error if p is zero, or with the currencies' exponents it
// passes what a ratio of uint64 holds.
func NewQuote[C Currency](from, to C, p Price) (Quote[C], error) {
	if p.V == 0 {
		return Quote[C]{}, fixed.ErrRange
	}
	q := Quote[C]{From: from, To: to, Price: p, num: p.V, den: 1}
	// v minor From is v·10^from.Exp major From, worth v·V·10^(from.Exp+p.Exp) major To,
	// which is v·V·10^k minor To.
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

// Convert returns v minor units of From in minor units of To, rounded by mode, exact
// through a 128-bit product; false if it passes 2^64-1, or mode is RoundExact and it
// is inexact. It panics on an invalid mode or a Quote not made by NewQuote.
func (q Quote[C]) Convert(v uint64, mode Round) (uint64, bool) {
	v, err := q.convert(v, mode)
	return v, err == 0
}

func (q *Quote[C]) convert(v uint64, mode Round) (uint64, fixed.Err) {
	q.mustValid()
	return fixed.MulDiv(v, q.num, q.den, mode)
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
	if i := x.index(q.From, q.To); i >= 0 {
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
		if x.quotes[i].From == from && x.quotes[i].To == to {
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

// Convert returns v minor units of from in minor units of to, rounded by mode, or v
// itself if from equals to. An error if x has no quote from from into to, the result
// passes 2^64-1, or mode is RoundExact and it is inexact. It panics on an invalid mode.
func (x *Exchange[C]) Convert(from, to C, v uint64, mode Round) (uint64, error) {
	fixed.CheckRound(mode)
	if from == to {
		return v, nil
	}
	q, ok := x.Quote(from, to)
	if !ok {
		return 0, errNoQuote
	}
	return result(q.convert(v, mode))
}

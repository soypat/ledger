package money

import (
	"math"
	"math/bits"
	"sync"

	"github.com/soypat/ledger/internal/fixed"
)

// Rates is a table of quotes between currency pairs, each one way, since a market's
// buy and sell quotes for a pair differ. It holds them in memory the caller gives
// [Rates.Configure]; the zero Rates holds none. It is not safe for concurrent use: share
// an [Exchange] instead.
type Rates[C Currency] struct {
	mu     sync.RWMutex
	quotes []Quote[C]
}

// Quote converts amounts of From into amounts of To at Price, in major units of To per
// major unit of From. Make one with [NewQuote]; the zero Quote is invalid.
type Quote[C Currency] struct {
	From, To C
	Price    Price
	num, den uint64 // minor units of To per minor unit of From
}

// Configure gives r buf to hold up to cap(buf) quotes, or keeps r's current memory if
// buf is nil, and removes every quote.
func (r *Rates[C]) Configure(buf []Quote[C]) {
	r.mu.Lock()
	if buf == nil {
		buf = r.quotes
	}
	buf = buf[:cap(buf)]
	clear(buf)
	r.quotes = buf[:0]
	r.mu.Unlock()
}

// Reset removes every quote and keeps r's memory.
func (r *Rates[C]) Reset() { r.Configure(nil) }

// Set adds q, replacing any quote with the same From and To; ErrCapacity if r is full.
// It panics on a Quote not made by NewQuote.
func (r *Rates[C]) Set(q Quote[C]) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	q.mustValid()
	if i := r.index(q.From, q.To); i >= 0 {
		r.quotes[i] = q
		return nil
	}
	if len(r.quotes) == cap(r.quotes) {
		return ErrCapacity
	}
	r.quotes = append(r.quotes, q)
	return nil
}

func (r *Rates[C]) index(from, to C) int {
	for i := range r.quotes {
		if r.quotes[i].From == from && r.quotes[i].To == to {
			return i
		}
	}
	return -1
}

// Quote returns the quote from from into to; false if r has none.
func (r *Rates[C]) Quote(from, to C) (Quote[C], bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	i := r.index(from, to)
	if i < 0 {
		return Quote[C]{}, false
	}
	return r.quotes[i], true
}

// Convert returns v minor units of from in minor units of to, rounded by mode, or v
// itself if from equals to. ErrNoQuote if r has no quote from from into to, ErrRange if
// the result passes 2^64-1, ErrInexact if mode is RoundExact and it is inexact. It
// panics on an invalid mode.
func (r *Rates[C]) Convert(from, to C, v uint64, mode Round) (uint64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	mode.check()
	if from == to {
		return v, nil
	}
	i := r.index(from, to)
	if i < 0 {
		return 0, ErrNoQuote
	}
	v, err := r.quotes[i].convert(v, mode)
	if err != 0 {
		return 0, err
	}
	return v, nil
}

// Price is an exact decimal V·10^Exp, as a market quotes an exchange rate: major units
// of one currency per major unit of another, such as 1185.50 ARS per USD.
type Price struct {
	V   uint64
	Exp int8
}

// ParsePrice parses decimal text such as "1185.50" exactly, keeping every digit:
// {118550, -2}. Exp is that of the last digit but at most 0, so "1.5e3" is {1500, 0}.
// Syntax errors are as in [ParseAmount]; ErrRange if negative, past 2^64-1, or Exp
// would pass an int8.
func ParsePrice(b []byte) (Price, error) {
	e, ferr := fixed.Exp(b)
	switch {
	case ferr == fixed.ErrSyntax:
		return Price{}, ErrSyntax
	case ferr != 0 || e < math.MinInt8:
		return Price{}, ErrRange
	}
	exp := int8(min(e, 0))
	v, err := parse(b, exp, RoundExact) // keeps every digit, so it is never inexact
	if err != nil {
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

// NewQuote returns the quote converting from into to at p, major units of to per
// major unit of from. ErrRange if p is zero, or with the currencies' exponents it
// passes what a ratio of uint64 holds.
func NewQuote[C Currency](from, to C, p Price) (Quote[C], error) {
	if p.V == 0 {
		return Quote[C]{}, ErrRange
	}
	q := Quote[C]{From: from, To: to, Price: p, num: p.V, den: 1}
	// v minor From is v·10^from.Exp major From, worth v·V·10^(from.Exp+p.Exp) major To,
	// which is v·V·10^k minor To.
	k := int(from.Exp()) + int(p.Exp) - int(to.Exp())
	if k >= 0 {
		m, ok := fixed.Pow10(k)
		hi, lo := bits.Mul64(p.V, m)
		if !ok || hi != 0 {
			return Quote[C]{}, ErrRange
		}
		q.num = lo
	} else {
		d, ok := fixed.Pow10(-k)
		if !ok {
			return Quote[C]{}, ErrRange
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

func (q Quote[C]) convert(v uint64, mode Round) (uint64, Err) {
	mode.check()
	q.mustValid()
	return mulDiv(v, q.num, q.den, mode)
}

func (q *Quote[C]) mustValid() {
	if q.den == 0 {
		panic("money: Quote not made by NewQuote")
	}
}

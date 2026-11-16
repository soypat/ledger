package ledger

import (
	"math"
	"strconv"
	"sync/atomic"
	"time"
)

// ID names an Account or Transfer. Caller-generated so a retried batch is idempotent;
// 0 and MaxUint64 are reserved.
type ID uint64

// ID layout written by IDGen: 42 bits of ticks of 2^20 ns (≈1.05 ms) since the unix
// epoch (until year 2116), 10 bits of writer and 12 bits of sequence within the tick.
const (
	idSeqBits    = 12
	idWriterBits = 10
	idTimeShift  = idSeqBits + idWriterBits
	idSeqMax     = 1<<idSeqBits - 1
	// MaxWriter is the largest writer number an IDGen can embed.
	MaxWriter = 1<<idWriterBits - 1
)

const idTickShift = 20 // a tick is 2^20 ns ≈ 1.05 ms; a shift, not a division

// Nanotime returns the unix time embedded by IDGen in nanoseconds, truncated to its
// tick of 2^20 ns. Meaningless for IDs from other sources.
func (id ID) Nanotime() int64 { return int64(id>>idTimeShift) << idTickShift }

// Time returns [ID.Nanotime] as a [time.Time].
func (id ID) Time() time.Time { return time.Unix(0, id.Nanotime()) }

// Writer returns the writer embedded by IDGen; meaningless for other IDs.
func (id ID) Writer() uint16 { return uint16(id>>idSeqBits) & MaxWriter }

// IsReserved reports whether id is 0 or MaxUint64, which name no record.
func (id ID) IsReserved() bool { return id == 0 || id == math.MaxUint64 }

// IDGen yields time-sortable IDs and is safe for concurrent use. IDs of distinct
// writers never collide; one writer's IDs only increase, even if its clock steps back.
type IDGen struct {
	last atomic.Uint64 // last ID issued: tick, writer and sequence in one word
}

// idTickMax is the last tick the 42-bit time field holds, early in the year 2116.
const idTickMax = 1<<(64-idTimeShift) - 1

// Next returns a fresh ID stamped with nanotime: unix nanoseconds, or any counter on a
// device without a clock. It exceeds every earlier ID of the same writer. It panics on a
// negative nanotime or one past the time field (year 2116), which would repeat IDs.
func (g *IDGen) Next(nanotime int64) ID {
	// A failed CAS means another caller got its ID, so with P callers a streak of k
	// failures has probability ≈ e^(-k/P). 1<<20 stays out of reach even at P=16384
	// (e^-64) and spins ~0.1 s, so reaching it means a bug, not load.
	const maxLoops = 1 << 20
	if nanotime < 0 {
		panic("ledger: IDGen.Next with negative nanotime")
	}
	tick := uint64(nanotime) >> idTickShift
	if tick > idTickMax {
		panic("ledger: IDGen.Next nanotime past the ID time field (year 2116)")
	}
	for i := 0; i < maxLoops; i++ {
		last := g.last.Load()
		lastTick, seq := last>>idTimeShift, last&idSeqMax
		switch {
		case tick > lastTick:
			lastTick, seq = tick, 0
		case seq < idSeqMax:
			seq++ // same tick, or clock stepped back
		case lastTick == idTickMax:
			panic("ledger: IDGen.Next exhausted the last tick of the ID time field")
		default:
			lastTick, seq = lastTick+1, 0 // sequence exhausted: borrow the next tick
		}
		next := lastTick<<idTimeShift | last&(MaxWriter<<idSeqBits) | seq
		if g.last.CompareAndSwap(last, next) {
			return ID(next)
		}
	}
	panic("ledger: IDGen.Next failed " + strconv.Itoa(maxLoops) + " CAS attempts in a row")
}

// SetWriter swaps the writer bits in place, so the sequence carries on.
func (g *IDGen) SetWriter(writer uint16) {
	if writer > MaxWriter {
		panic("ledger: writer exceeds MaxWriter")
	}
	for {
		last := g.last.Load()
		if g.last.CompareAndSwap(last, last&^(MaxWriter<<idSeqBits)|uint64(writer)<<idSeqBits) {
			return
		}
	}
}

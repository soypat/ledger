package ledger

import (
	"context"
	"io"
)

// ── Client API: what application code is written against ──

// Writer submits batches. results[i] receives the outcome of batch[i]; a length
// mismatch returns ErrLengthMismatch before anything is submitted.
// On any other error the outcome is unknown: resubmit the same batch.
type Writer interface {
	CreateAccounts(ctx context.Context, results []Result, batch []Account) error
	CreateTransfers(ctx context.Context, results []Result, batch []Transfer) error
}

// Reader fetches records by ID, appending those found to dst.
type Reader interface {
	LookupAccounts(ctx context.Context, dst []Account, ids []ID) ([]Account, error)
	LookupTransfers(ctx context.Context, dst []Transfer, ids []ID) ([]Transfer, error)
}

// Querier scans by filter. Optional: a small device ledger may leave it out.
// Results page by timestamp: set TimestampMin past the last result to continue.
type Querier interface {
	GetAccountTransfers(ctx context.Context, dst []Transfer, f AccountFilter) ([]Transfer, error)
	GetAccountBalances(ctx context.Context, dst []AccountBalance, f AccountFilter) ([]AccountBalance, error)
	QueryAccounts(ctx context.Context, dst []Account, f QueryFilter) ([]Account, error)
	QueryTransfers(ctx context.Context, dst []Transfer, f QueryFilter) ([]Transfer, error)
}

// Ledger is what application code is written against.
type Ledger interface {
	Writer
	Reader
}

// Capper is optionally implemented by a Ledger to report its limits.
type Capper interface {
	Caps() Caps
}

// Caps reports a ledger's limits. Zero means unbounded.
type Caps struct {
	MaxBatch     int // events per call: message size on a wire, RAM on a device
	MaxAccounts  int
	MaxTransfers int
}

// ── Deterministic core: the contract every node runs ──

// StateMachine is the deterministic core: the same ops in the same order give
// the same state and replies on every node. No clock, no randomness, no I/O.
type StateMachine interface {
	// Apply executes a write stamped ts by the sequencer; event i gets ts+i.
	// The reply, one Result per event in batch order, is appended to dst.
	Apply(dst []byte, op Op, ts int64, body []byte) []byte
	// Read executes a read-only op against current state, appending the reply to dst.
	Read(dst []byte, op Op, body []byte) []byte
}

// Snapshotter saves and loads whole state, so a journal can be truncated
// and a lagging replica can catch up.
type Snapshotter interface {
	Snapshot(w io.Writer) error
	Restore(r io.Reader) error
}

// ── Storage under the rules ──

// Store holds the records the rules read and write. Single-threaded by contract.
// Reads cannot fail; inserts report false when a fixed-capacity store is full.
type Store interface {
	Account(id ID) (Account, bool)
	InsertAccount(a *Account) bool
	// UpdateAccount overwrites the counters and closed flag of an existing account.
	UpdateAccount(a *Account)
	Transfer(id ID) (Transfer, bool)
	InsertTransfer(t *Transfer) bool
	PendingStatus(id ID) PendingStatus
	SetPendingStatus(id ID, s PendingStatus)
	// ScopeOpen starts a linked chain; ScopeClose(false) discards every write since.
	ScopeOpen()
	ScopeClose(persist bool)
}

// Scanner is optionally implemented by a Store to back Querier.
// Each call appends matches in timestamp order until dst is full.
type Scanner interface {
	ScanAccounts(dst []Account, f *QueryFilter) []Account
	ScanTransfers(dst []Transfer, f *QueryFilter) []Transfer
	ScanAccountTransfers(dst []Transfer, f *AccountFilter) []Transfer
}

// ── Ordering and durability on one node ──

// Entry is one sequenced write: the unit of journaling and replication.
type Entry struct {
	Seq       uint64 // gapless, from 1
	Timestamp int64  // stamps the first event of the batch
	Op        Op
	Body      []byte // encoded records; aliases a caller buffer
}

// Journal makes entries durable on one node: a file, a flash region,
// or the write-ahead log inside a consensus implementation.
type Journal interface {
	// Append buffers e; it is durable only after Sync.
	Append(e Entry) error
	// Sync makes every appended entry durable. One Sync may cover many Appends.
	Sync() error
	// Next reads the entry after seq into buf; io.EOF past the last one.
	Next(seq uint64, buf []byte) (Entry, error)
	// Truncate drops entries through seq once a snapshot covers them.
	Truncate(seq uint64) error
}

// ── Network ──

// Transport carries one encoded request to a node and appends the reply to dst.
// It owns framing, reconnects and node selection.
type Transport interface {
	RoundTrip(ctx context.Context, dst []byte, op Op, body []byte) ([]byte, error)
}

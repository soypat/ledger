package ledger

import (
	"math"
	"time"
)

// Transfer moves Amount from the debit to the credit account. Immutable once
// accepted; mistakes are corrected by new transfers.
type Transfer struct {
	ID              ID
	DebitAccountID  ID
	CreditAccountID ID
	PendingID       ID     // pending transfer to post or void; else zero
	Amount          uint64 // minor units; on post, AmountMax posts the full pending amount
	UserData64      uint64 // free for caller; zero on post/void inherits pending's
	UserData32      uint32 // free for caller; zero on post/void inherits pending's
	Ledger          uint32 // must match both accounts
	Code            uint16 // caller-defined kind; nonzero
	Flags           TransferFlags
	Timestamp       int64 // unix nanos set by Ledger; caller sets only with TransferImported
}

// IsPending reports whether t reserves its amount until a post or void resolves it.
func (t *Transfer) IsPending() bool { return t.Flags.Has(TransferPending) }

// IsResolving reports whether t posts or voids the pending transfer PendingID.
func (t *Transfer) IsResolving() bool { return t.Flags.Has(TransferPostPending | TransferVoidPending) }

// TransferBuilder fills IDs, ledger and code so callers state only what moves.
// The zero value is valid: wall clock, writer 0, ledger and code unset.
type TransferBuilder struct {
	ids       IDGen
	_nanotime func() int64
	ledger    uint32
	code      uint16
}

// TransferBuilderConfig configures a [TransferBuilder].
type TransferBuilderConfig struct {
	Nanotime func() int64 // stamps IDs; nil uses the wall clock
	Ledger   uint32       // ledger of transfers built by Transfer and Pending
	Code     uint16       // default code; set Transfer.Code on the result to override
	Writer   uint16       // embedded in IDs so concurrent writers never collide; at most MaxWriter
}

// Configure replaces the builder's settings; its ID sequence carries on, so IDs
// never repeat. It panics if cfg.Writer exceeds MaxWriter.
func (b *TransferBuilder) Configure(cfg TransferBuilderConfig) {
	b.ids.SetWriter(cfg.Writer)
	b._nanotime, b.ledger, b.code = cfg.Nanotime, cfg.Ledger, cfg.Code
}

// ID returns a fresh ID, e.g. for an account created alongside transfers.
func (b *TransferBuilder) ID() ID {
	return b.ids.Next(b.nanotime())
}

func (b *TransferBuilder) nanotime() int64 {
	if b._nanotime == nil {
		return time.Now().UnixNano()
	}
	return b._nanotime()
}

// Transfer returns a transfer of amount from debit to credit, posted at once.
func (b *TransferBuilder) Transfer(debit, credit ID, amount uint64) Transfer {
	return Transfer{
		ID:              b.ID(),
		DebitAccountID:  debit,
		CreditAccountID: credit,
		Amount:          amount,
		Ledger:          b.ledger,
		Code:            b.code,
	}
}

// Pending returns a transfer reserving amount until a Post or Void resolves it.
func (b *TransferBuilder) Pending(debit, credit ID, amount uint64) Transfer {
	t := b.Transfer(debit, credit, amount)
	t.Flags = TransferPending
	return t
}

// Post returns a transfer committing amount of pending, AmountMax meaning all of it;
// the rest of the reservation is released. Zero UserData inherits pending's.
// It panics if pending is not a pending transfer or amount exceeds its reservation.
func (b *TransferBuilder) Post(pending *Transfer, amount uint64) Transfer {
	if !pending.IsPending() {
		panic("ledger: Post of a transfer that is not pending")
	}
	if amount > pending.Amount && amount != AmountMax {
		panic("ledger: Post amount exceeds the pending reservation")
	}
	return b.resolve(pending, amount, TransferPostPending)
}

// Void returns a transfer releasing pending's whole reservation.
// It panics if pending is not a pending transfer.
func (b *TransferBuilder) Void(pending *Transfer) Transfer {
	if !pending.IsPending() {
		panic("ledger: Void of a transfer that is not pending")
	}
	return b.resolve(pending, pending.Amount, TransferVoidPending)
}

func (b *TransferBuilder) resolve(p *Transfer, amount uint64, f TransferFlags) Transfer {
	return Transfer{
		ID:              b.ID(),
		DebitAccountID:  p.DebitAccountID,
		CreditAccountID: p.CreditAccountID,
		PendingID:       p.ID,
		Amount:          amount,
		Ledger:          p.Ledger,
		Code:            p.Code,
		Flags:           f,
	}
}

// Reverse returns a transfer undoing posted t by moving its amount back, keeping
// t's code and UserData. t must be as stored by a ledger; Reverse panics on a pending t.
func (b *TransferBuilder) Reverse(t *Transfer) Transfer {
	if t.Flags.Has(TransferPending) {
		panic("ledger: Reverse of a pending transfer; use Void")
	}
	return Transfer{
		ID:              b.ID(),
		DebitAccountID:  t.CreditAccountID,
		CreditAccountID: t.DebitAccountID,
		Amount:          t.Amount,
		UserData64:      t.UserData64,
		UserData32:      t.UserData32,
		Ledger:          t.Ledger,
		Code:            t.Code,
	}
}

// Validate checks t's fields for submission without looking anything up and returns zero
// if they are valid, else the first broken rule in TigerBeetle's create_transfers order.
// A post or void may leave accounts, ledger, code and UserData zero to inherit them.
// Checks needing a store are Execute's and the ledger's.
func (t *Transfer) Validate() ErrCode {
	if code := validateTimestamp(t.Timestamp, t.Flags.Has(TransferImported)); code != 0 {
		return code
	}
	switch {
	case t.Flags&^transferFlagsKnown != 0:
		return ErrReservedFlag
	case t.ID == 0:
		return ErrIDMustNotBeZero
	case t.ID == math.MaxUint64:
		return ErrIDMustNotBeIntMax
	}
	if t.IsResolving() {
		const exclusive = TransferPending | TransferBalancingDebit | TransferBalancingCredit |
			TransferClosingDebit | TransferClosingCredit
		switch {
		case t.Flags.Has(exclusive), t.Flags.Has(TransferPostPending) && t.Flags.Has(TransferVoidPending):
			return ErrFlagsAreMutuallyExclusive
		case t.PendingID == 0:
			return ErrPendingIDMustNotBeZero
		case t.PendingID == math.MaxUint64:
			return ErrPendingIDMustNotBeIntMax
		case t.PendingID == t.ID:
			return ErrPendingIDMustBeDifferent
		}
		return 0
	}
	switch {
	case t.DebitAccountID == 0:
		return ErrDebitAccountIDMustNotBeZero
	case t.DebitAccountID == math.MaxUint64:
		return ErrDebitAccountIDMustNotBeIntMax
	case t.CreditAccountID == 0:
		return ErrCreditAccountIDMustNotBeZero
	case t.CreditAccountID == math.MaxUint64:
		return ErrCreditAccountIDMustNotBeIntMax
	case t.DebitAccountID == t.CreditAccountID:
		return ErrAccountsMustBeDifferent
	case t.PendingID != 0:
		return ErrPendingIDMustBeZero
	case !t.IsPending() && t.Flags.Has(TransferClosingDebit|TransferClosingCredit):
		return ErrClosingTransferMustBePending
	case t.Ledger == 0:
		return ErrLedgerMustNotBeZero
	case t.Code == 0:
		return ErrCodeMustNotBeZero
	}
	return 0
}

package ledger

import "math"

// Account accumulates the effect of transfers. After creation only Balance
// and the closed flag change, and only through transfers.
type Account struct {
	ID         ID
	Balance           // zero on create
	UserData64 uint64 // free for caller
	UserData32 uint32 // free for caller
	Ledger     uint32 // accounting boundary, e.g. a currency, not a shard; transfers stay within one
	Code       uint16 // caller-defined kind; nonzero
	Flags      AccountFlags
	Timestamp  int64 // unix nanos set by Ledger; caller sets only with AccountImported
}

// AccountFilter selects one account's transfers or balance history.
// Zero-valued fields match anything.
type AccountFilter struct {
	AccountID    ID
	UserData64   uint64
	UserData32   uint32
	Limit        uint32 // max results; zero is unlimited
	Code         uint16
	Flags        FilterFlags
	TimestampMin int64 // inclusive
	TimestampMax int64 // inclusive; zero is unbounded
}

// QueryFilter selects accounts or transfers by their indexed fields.
// Zero-valued fields match anything.
type QueryFilter struct {
	UserData64   uint64
	UserData32   uint32
	Ledger       uint32
	Code         uint16
	Flags        FilterFlags // only FilterReversed applies
	Limit        uint32      // max results; zero is unlimited
	TimestampMin int64       // inclusive
	TimestampMax int64       // inclusive; zero is unbounded
}

// Validate checks a's fields for creation without looking anything up and returns zero
// if they are valid, else the first broken rule in TigerBeetle's create_accounts order.
// Checks needing a store (exists, imported timestamp regressing) are the ledger's.
func (a *Account) Validate() ErrCode {
	if code := validateTimestamp(a.Timestamp, a.Flags.Has(AccountImported)); code != 0 {
		return code
	}
	switch {
	case a.Flags&^accountFlagsKnown != 0:
		return ErrReservedFlag
	case a.ID == 0:
		return ErrIDMustNotBeZero
	case a.ID == math.MaxUint64:
		return ErrIDMustNotBeIntMax
	case a.Flags.Has(AccountDebitsMustNotExceedCredits) && a.Flags.Has(AccountCreditsMustNotExceedDebits):
		return ErrFlagsAreMutuallyExclusive
	case a.DebitsPending != 0:
		return ErrDebitsPendingMustBeZero
	case a.DebitsPosted != 0:
		return ErrDebitsPostedMustBeZero
	case a.CreditsPending != 0:
		return ErrCreditsPendingMustBeZero
	case a.CreditsPosted != 0:
		return ErrCreditsPostedMustBeZero
	case a.Ledger == 0:
		return ErrLedgerMustNotBeZero
	case a.Code == 0:
		return ErrCodeMustNotBeZero
	}
	return 0
}

package ledger

import (
	"math/bits"
	"strconv"
	"unsafe"
)

// enum matches enum-like types to definitions.go. Generate their string methods running `go generate ./...`
//
//go:generate stringer -type=Side,accountBits,transferBits,filterBits,PendingStatus,ErrCode,Op,errGeneric -linecomment -output stringers.go
type enum interface {
	~uint64 | ~uint32 | ~uint16 | ~uint8
	String() string
}

// Masks of every defined flag bit; Validate rejects records carrying others.
const (
	accountFlagsKnown  = AccountFlags(1<<accountBitsMax - 1)
	transferFlagsKnown = TransferFlags(1<<transferBitsMax - 1)
)

// Compile-time layout checks: a field change that grows a record fails the build.
var (
	_ [72]byte = [unsafe.Sizeof(Account{})]byte{}
	_ [72]byte = [unsafe.Sizeof(Transfer{})]byte{}
	_ [16]byte = [unsafe.Sizeof(Result{})]byte{}
	_ [48]byte = [unsafe.Sizeof(AccountFilter{})]byte{}
	_ [40]byte = [unsafe.Sizeof(QueryFilter{})]byte{}
)

// AmountMax as a post's Amount posts the full pending amount, as TigerBeetle's
// AMOUNT_MAX; zero posts nothing and releases the whole reservation. As a balancing
// transfer's Amount it moves as much as the account allows.
const AmountMax = 1<<64 - 1

// Result is the outcome of one event. A batch gets one Result per event in batch
// order, so an event's index is its Result's position, as in TigerBeetle 0.17.
type Result struct {
	// Timestamp of the created record, of the original record on ErrExists,
	// otherwise of when validation ran.
	Timestamp int64
	Code      ErrCode // zero if the event applied
}

// ErrorAppend is a no-alloc version of [error.Error], appending "ok" if the event
// applied. golang/go/issues/83013
func (r *Result) ErrorAppend(dst []byte) []byte {
	switch {
	case r.Code == 0:
		return append(dst, "ok"...)
	case r.Code >= _errMax:
		return append(dst, "invalid result code"...)
	}
	return append(dst, r.Code.String()...)
}

// Side is the side that increases an account's balance.
type Side uint8

const (
	// cash, expenses, external payers
	DebitNormal Side = iota + 1 // debit normal
	// liabilities, income, external payees
	CreditNormal // credit normal
)

// Net returns debits less credits for a debit-normal side, credits less debits for a
// credit-normal one; false if the result does not fit an int64. Panics on an invalid s.
func (s Side) Net(debits, credits uint64) (int64, bool) {
	return s.net(u128{lo: debits}, u128{lo: credits})
}

// net is Net over 128-bit debits and credits, so sums of counters can be netted
// without overflowing first. Exact: false only if the result does not fit an int64.
func (s Side) net(debits, credits u128) (int64, bool) {
	switch s {
	case DebitNormal:
	case CreditNormal:
		debits, credits = credits, debits
	default:
		panic("ledger: invalid Side " + strconv.Itoa(int(s)))
	}
	negative := debits.less(credits)
	if negative {
		debits, credits = credits, debits
	}
	d := debits.sub(credits)
	switch {
	case d.hi != 0, negative && d.lo > 1<<63, !negative && d.lo > 1<<63-1:
		return 0, false
	case negative:
		return -int64(d.lo), true // d.lo == 1<<63 wraps to MinInt64, which is exact
	}
	return int64(d.lo), true
}

// accountBits names single AccountFlags bits so stringer can generate String.
type accountBits uint16

const (
	_accountLinked                     accountBits = 1 << iota // linked
	_accountDebitsMustNotExceedCredits                         // debits<=credits
	_accountCreditsMustNotExceedDebits                         // credits<=debits
	_accountClosed                                             // closed
	_accountImported                                           // imported

	accountBitsMax = iota // number of defined bits; untyped so stringer skips it
)

// AccountFlags declare constraints the Ledger enforces on every transfer.
type AccountFlags uint16

const (
	// all-or-nothing with next account in batch
	AccountLinked = AccountFlags(_accountLinked)
	// debits pending+posted ≤ credits posted
	AccountDebitsMustNotExceedCredits = AccountFlags(_accountDebitsMustNotExceedCredits)
	// credits pending+posted ≤ debits posted
	AccountCreditsMustNotExceedDebits = AccountFlags(_accountCreditsMustNotExceedDebits)
	// rejects transfers; set by a closing transfer
	AccountClosed = AccountFlags(_accountClosed)
	// caller supplies Timestamp (backfill)
	AccountImported = AccountFlags(_accountImported)
)

func (af AccountFlags) Has(x AccountFlags) bool { return af&x != 0 }

func (af AccountFlags) StringAppend(dst []byte) []byte {
	return stringAppendBits(dst, accountBits(af))
}

func (af AccountFlags) String() string { return string(af.StringAppend(nil)) }

// transferBits names single TransferFlags bits so stringer can generate String.
type transferBits uint16

const (
	_transferLinked          transferBits = 1 << iota // linked
	_transferPending                                  // pending
	_transferPostPending                              // post
	_transferVoidPending                              // void
	_transferBalancingDebit                           // balancing-debit
	_transferBalancingCredit                          // balancing-credit
	_transferClosingDebit                             // closing-debit
	_transferClosingCredit                            // closing-credit
	_transferImported                                 // imported

	transferBitsMax = iota // number of defined bits; untyped so stringer skips it
)

// TransferFlags select a transfer's lifecycle and execution behaviour.
type TransferFlags uint16

const (
	// all-or-nothing with next transfer in batch
	TransferLinked = TransferFlags(_transferLinked)
	// reserve Amount in *Pending counters
	TransferPending = TransferFlags(_transferPending)
	// commit PendingID up to its amount; rest released
	TransferPostPending = TransferFlags(_transferPostPending)
	// release PendingID entirely
	TransferVoidPending = TransferFlags(_transferVoidPending)
	// clamp Amount to what the debit account allows
	TransferBalancingDebit = TransferFlags(_transferBalancingDebit)
	// clamp Amount to what the credit account allows
	TransferBalancingCredit = TransferFlags(_transferBalancingCredit)
	// with Pending: closes debit account; voiding reopens it
	TransferClosingDebit = TransferFlags(_transferClosingDebit)
	// with Pending: closes credit account; voiding reopens it
	TransferClosingCredit = TransferFlags(_transferClosingCredit)
	// caller supplies Timestamp (backfill)
	TransferImported = TransferFlags(_transferImported)
)

func (tf TransferFlags) Has(x TransferFlags) bool { return tf&x != 0 }

func (tf TransferFlags) StringAppend(dst []byte) []byte {
	return stringAppendBits(dst, transferBits(tf))
}

func (tf TransferFlags) String() string { return string(tf.StringAppend(nil)) }

// filterBits names single FilterFlags bits so stringer can generate String.
type filterBits uint8

const (
	_filterDebits   filterBits = 1 << iota // debits
	_filterCredits                         // credits
	_filterReversed                        // reversed
)

// FilterFlags narrow and order query results.
type FilterFlags uint8

const (
	// transfers debiting the account
	FilterDebits = FilterFlags(_filterDebits)
	// transfers crediting the account
	FilterCredits = FilterFlags(_filterCredits)
	// newest first
	FilterReversed = FilterFlags(_filterReversed)
)

func (ff FilterFlags) Has(x FilterFlags) bool { return ff&x != 0 }

func (ff FilterFlags) StringAppend(dst []byte) []byte {
	return stringAppendBits(dst, filterBits(ff))
}

func (ff FilterFlags) String() string { return string(ff.StringAppend(nil)) }

// PendingStatus is the lifecycle state of a pending transfer.
type PendingStatus uint8

const (
	// ID is not a pending transfer
	PendingNone PendingStatus = iota // none
	// awaiting post or void
	PendingActive // active
	PendingPosted // posted
	PendingVoided // voided

	_pendingMax = iota
)

// ErrCode is why an event was rejected. Names and meanings follow TigerBeetle's
// create_accounts and create_transfers results; each operation checks them in
// TigerBeetle's order of precedence, which is not the order of these constants.
// uint8 backing keeps returning one allocation free.
type ErrCode uint8

const (
	_ ErrCode = iota
	// another event in the same linked chain failed
	ErrLinkedEventFailed // linked event failed
	// last event of the batch carries a Linked flag
	ErrLinkedEventChainOpen // linked event chain open
	// batch mixes imported and non-imported events
	ErrImportedEventExpected    // imported event expected
	ErrImportedEventNotExpected // imported event not expected
	// Timestamp set without the Imported flag
	ErrTimestampMustBeZero // timestamp must be zero
	// imported Timestamp not positive
	ErrImportedEventTimestampOutOfRange // imported event timestamp out of range
	// imported Timestamp later than the ledger clock
	ErrImportedEventTimestampMustNotAdvance // imported event timestamp must not advance
	// a flag bit no constant defines
	ErrReservedFlag      // reserved flag
	ErrIDMustNotBeZero   // id must not be zero
	ErrIDMustNotBeIntMax // id must not be int max
	// same ID already accepted with the named field different: caller bug
	ErrExistsWithDifferentFlags           // exists with different flags
	ErrExistsWithDifferentPendingID       // exists with different pending id
	ErrExistsWithDifferentDebitAccountID  // exists with different debit account id
	ErrExistsWithDifferentCreditAccountID // exists with different credit account id
	ErrExistsWithDifferentAmount          // exists with different amount
	ErrExistsWithDifferentUserData64      // exists with different user data 64
	ErrExistsWithDifferentUserData32      // exists with different user data 32
	ErrExistsWithDifferentLedger          // exists with different ledger
	ErrExistsWithDifferentCode            // exists with different code
	// identical event already accepted; on its own, treat as success
	ErrExists                    // exists
	ErrFlagsAreMutuallyExclusive // flags are mutually exclusive
	// account created with a nonzero counter
	ErrDebitsPendingMustBeZero        // debits pending must be zero
	ErrDebitsPostedMustBeZero         // debits posted must be zero
	ErrCreditsPendingMustBeZero       // credits pending must be zero
	ErrCreditsPostedMustBeZero        // credits posted must be zero
	ErrDebitAccountIDMustNotBeZero    // debit account id must not be zero
	ErrDebitAccountIDMustNotBeIntMax  // debit account id must not be int max
	ErrCreditAccountIDMustNotBeZero   // credit account id must not be zero
	ErrCreditAccountIDMustNotBeIntMax // credit account id must not be int max
	ErrAccountsMustBeDifferent        // accounts must be different
	// PendingID set on a transfer that neither posts nor voids
	ErrPendingIDMustBeZero      // pending id must be zero
	ErrPendingIDMustNotBeZero   // pending id must not be zero
	ErrPendingIDMustNotBeIntMax // pending id must not be int max
	// a post or void naming itself as its pending transfer
	ErrPendingIDMustBeDifferent                // pending id must be different
	ErrClosingTransferMustBePending            // closing transfer must be pending
	ErrLedgerMustNotBeZero                     // ledger must not be zero
	ErrCodeMustNotBeZero                       // code must not be zero
	ErrDebitAccountNotFound                    // debit account not found
	ErrCreditAccountNotFound                   // credit account not found
	ErrAccountsMustHaveTheSameLedger           // accounts must have the same ledger
	ErrTransferMustHaveTheSameLedgerAsAccounts // transfer must have the same ledger as accounts
	ErrPendingTransferNotFound                 // pending transfer not found
	ErrPendingTransferNotPending               // pending transfer not pending
	// a post or void field, nonzero, differs from the pending transfer's
	ErrPendingTransferHasDifferentDebitAccountID  // pending transfer has different debit account id
	ErrPendingTransferHasDifferentCreditAccountID // pending transfer has different credit account id
	ErrPendingTransferHasDifferentLedger          // pending transfer has different ledger
	ErrPendingTransferHasDifferentCode            // pending transfer has different code
	// post or void amount above the pending amount
	ErrExceedsPendingTransferAmount // exceeds pending transfer amount
	// void amount, nonzero, below the pending amount
	ErrPendingTransferHasDifferentAmount // pending transfer has different amount
	ErrPendingTransferAlreadyPosted      // pending transfer already posted
	ErrPendingTransferAlreadyVoided      // pending transfer already voided
	// imported Timestamp not after the ledger's last event
	ErrImportedEventTimestampMustNotRegress // imported event timestamp must not regress
	// imported Timestamp not after the named account's creation
	ErrImportedEventTimestampMustPostdateDebitAccount  // imported event timestamp must postdate debit account
	ErrImportedEventTimestampMustPostdateCreditAccount // imported event timestamp must postdate credit account
	ErrDebitAccountAlreadyClosed                       // debit account already closed
	ErrCreditAccountAlreadyClosed                      // credit account already closed
	// the named counter, plus the amount, would pass 2^64
	ErrOverflowsDebitsPending  // overflows debits pending
	ErrOverflowsCreditsPending // overflows credits pending
	ErrOverflowsDebitsPosted   // overflows debits posted
	ErrOverflowsCreditsPosted  // overflows credits posted
	// pending plus posted, plus the amount, would pass 2^64
	ErrOverflowsDebits  // overflows debits
	ErrOverflowsCredits // overflows credits
	// violates AccountDebitsMustNotExceedCredits
	ErrExceedsCredits // exceeds credits
	// violates AccountCreditsMustNotExceedDebits
	ErrExceedsDebits // exceeds debits
	// a fixed-capacity Store is full; not a TigerBeetle code
	ErrCapacity // capacity exceeded

	_errMax = iota // not ErrCode type to prevent stringers from picking up.
)

func (e ErrCode) Error() string { return e.String() }

// validateTimestamp checks a submitted timestamp: zero unless imported, positive if so.
func validateTimestamp(ts int64, imported bool) ErrCode {
	switch {
	case !imported && ts != 0:
		return ErrTimestampMustBeZero
	case imported && ts <= 0:
		return ErrImportedEventTimestampOutOfRange
	}
	return 0
}

// errGeneric is a failure of the call itself rather than of an event, as in kui.
// Returning one does not allocate.
type errGeneric uint8

const (
	_ errGeneric = iota
	// results and batch lengths differ; nothing was submitted
	ErrLengthMismatch // length mismatch
)

func (e errGeneric) Error() string { return e.String() }

// Op names a ledger operation in journals and on the wire.
type Op uint8

const (
	OpCreateAccounts      Op = iota + 1 // create accounts
	OpCreateTransfers                   // create transfers
	OpLookupAccounts                    // lookup accounts
	OpLookupTransfers                   // lookup transfers
	OpGetAccountTransfers               // get account transfers
	OpGetAccountBalances                // get account balances
	OpQueryAccounts                     // query accounts
	OpQueryTransfers                    // query transfers
)

// IsWrite reports whether op changes state and so must be sequenced and journaled.
func (op Op) IsWrite() bool { return op == OpCreateAccounts || op == OpCreateTransfers }

// stringAppendBits appends the name of every set bit of v, separated by '|'.
func stringAppendBits[T enum](dst []byte, v T) []byte {
	appended := false
	for i := 0; v != 0; i++ {
		if v&1 != 0 {
			if appended {
				dst = append(dst, '|')
			}
			dst = append(dst, T(1<<i).String()...)
			appended = true
		}
		v >>= 1
	}
	return dst
}

// u128 is an unsigned 128-bit integer: the exact sum of two uint64 counters, such as
// pending plus posted, so it can be compared and netted without overflowing first.
type u128 struct{ hi, lo uint64 }

// sum64 returns x+y exactly.
func sum64(x, y uint64) u128 {
	lo, carry := bits.Add64(x, y, 0)
	return u128{hi: carry, lo: lo}
}

func (x u128) less(y u128) bool { return x.hi < y.hi || (x.hi == y.hi && x.lo < y.lo) }

// sub returns x-y; x must not be less than y.
func (x u128) sub(y u128) u128 {
	lo, borrow := bits.Sub64(x.lo, y.lo, 0)
	hi, _ := bits.Sub64(x.hi, y.hi, borrow)
	return u128{hi: hi, lo: lo}
}

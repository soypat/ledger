package ledger

import (
	"cmp"
	"math/bits"
)

// ExecuteReplay applies accepted, a record as a ledger stored it, to acc, which must be
// its debit or credit account: the counters of acc's side and, for a closing transfer,
// acc's closed flag. Replaying an account's records in order rebuilds it from its state
// at creation. pending is the transfer accepted posts or voids, nil if it resolves none.
//
// A nonzero code means acc's history is inconsistent with accepted, such as a post of a
// reservation acc never got or a transfer on an account closed at that point; acc is
// then left unchanged. It panics if acc is not one of accepted's accounts or pending is
// not the transfer accepted resolves, which are caller bugs.
func ExecuteReplay(acc *Account, accepted, pending *Transfer) ErrCode {
	isVoid := accepted.Flags.Has(TransferVoidPending)
	var reserved uint64
	if accepted.IsResolving() {
		// A stored post or void carries the amount and accounts resolved from pending.
		switch {
		case pending == nil:
			panic("ledger: replay of a post or void without its pending transfer")
		case pending.ID != accepted.PendingID:
			panic("ledger: replay got a pending transfer other than the one accepted resolves")
		case !pending.IsPending():
			panic("ledger: replay of a post or void of a transfer that is not pending")
		case accepted.DebitAccountID != pending.DebitAccountID || accepted.CreditAccountID != pending.CreditAccountID:
			panic("ledger: replay of a post or void with accounts other than its pending transfer's")
		case accepted.Amount > pending.Amount || isVoid && accepted.Amount != pending.Amount:
			panic("ledger: replay of a post or void with an amount its pending transfer does not allow")
		case !isVoid && pending.Flags.Has(TransferClosingDebit|TransferClosingCredit):
			panic("ledger: replay of a post of a closing transfer, which only a void resolves")
		}
		reserved = pending.Amount
	} else if pending != nil {
		panic("ledger: replay got a pending transfer for a record that resolves none")
	}

	var pend, post *uint64
	var debit bool
	var closes TransferFlags // the closing flag that acts on acc's side
	switch acc.ID {
	case accepted.DebitAccountID:
		pend, post, debit, closes = &acc.DebitsPending, &acc.DebitsPosted, true, TransferClosingDebit
	case accepted.CreditAccountID:
		pend, post, debit, closes = &acc.CreditsPending, &acc.CreditsPosted, false, TransferClosingCredit
	default:
		panic("ledger: replay onto an account the record does not name")
	}
	closed := acc.Flags.Has(AccountClosed)
	reopens := isVoid && pending.Flags.Has(closes)
	switch {
	case closed && !isVoid: // as Execute: a closed account takes no movement but a void
		return pick(debit, ErrDebitAccountAlreadyClosed, ErrCreditAccountAlreadyClosed)
	case reopens && !closed: // the closing transfer was never replayed onto acc
		return ErrPendingTransferNotFound
	}
	if code := exec(pend, post, accepted, reserved, debit); code != 0 {
		return code
	}
	switch {
	case accepted.Flags.Has(closes):
		acc.Flags |= AccountClosed
	case reopens:
		acc.Flags &^= AccountClosed
	}
	return 0
}

// Execute validates submitted against its accounts dr and cr and, if rules allow it,
// applies it to their counters and closed flags. It returns zero on success, else the
// first broken rule in TigerBeetle's create_transfers order, leaving submitted, dr and
// cr unchanged. On success it overwrites *submitted with the record to store: amount,
// accounts, ledger, code and UserData resolved, Timestamp set.
//
// ts is the ledger's clock for this event, positive; it stamps the record unless
// submitted is imported, which carries its own. dr and cr are nil if not found; for a
// post or void they are pending's accounts. pending is the transfer submitted posts or
// voids, nil if not found or if submitted resolves none, and status is its lifecycle
// state. Checks needing a store (exists, imported timestamps against the ledger clock)
// are the ledger's. It panics on arguments inconsistent with submitted, which are
// caller bugs.
func Execute(dr, cr *Account, submitted, pending *Transfer, status PendingStatus, ts int64) ErrCode {
	if ts <= 0 {
		panic("ledger: execute on non-positive timestamp")
	}
	if code := submitted.Validate(); code != 0 {
		return code
	}
	stamp := ts
	if submitted.Flags.Has(TransferImported) {
		stamp = submitted.Timestamp
	}
	if submitted.IsResolving() {
		return executeResolve(dr, cr, submitted, pending, status, stamp)
	}
	if pending != nil {
		panic("ledger: execute got a pending transfer for a transfer that resolves none")
	}
	return executeCreate(dr, cr, submitted, stamp)
}

// executeResolve posts or voids pend, in post_or_void_pending_transfer's order.
func executeResolve(dr, cr *Account, submitted, pend *Transfer, status PendingStatus, stamp int64) ErrCode {
	isPost := submitted.Flags.Has(TransferPostPending)
	isVoid := submitted.Flags.Has(TransferVoidPending)
	if isPost == isVoid {
		panic("ledger: execute of a resolve that does not either post or void")
	}
	if pend == nil {
		return ErrPendingTransferNotFound
	}
	if pend.ID != submitted.PendingID {
		panic("ledger: execute got a pending transfer other than the one submitted resolves")
	}
	if !pend.IsPending() {
		return ErrPendingTransferNotPending
	}
	if dr == nil || cr == nil || dr.ID != pend.DebitAccountID || cr.ID != pend.CreditAccountID {
		panic("ledger: execute got accounts other than the pending transfer's")
	}
	// A post of AmountMax posts it all; a void of zero voids it all. Otherwise exact amount.
	amount := submitted.Amount
	if (isPost && amount == AmountMax) || (isVoid && amount == 0) {
		amount = pend.Amount
	}
	imported := submitted.Flags.Has(TransferImported)
	switch {
	case firstNzMismatch(submitted.DebitAccountID, pend.DebitAccountID):
		return ErrPendingTransferHasDifferentDebitAccountID
	case firstNzMismatch(submitted.CreditAccountID, pend.CreditAccountID):
		return ErrPendingTransferHasDifferentCreditAccountID
	case firstNzMismatch(submitted.Ledger, pend.Ledger):
		return ErrPendingTransferHasDifferentLedger
	case firstNzMismatch(submitted.Code, pend.Code):
		return ErrPendingTransferHasDifferentCode
	case amount > pend.Amount:
		return ErrExceedsPendingTransferAmount
	case isVoid && amount < pend.Amount:
		return ErrPendingTransferHasDifferentAmount
	case status == PendingPosted:
		return ErrPendingTransferAlreadyPosted
	case status == PendingVoided:
		return ErrPendingTransferAlreadyVoided
	case status != PendingActive:
		panic("ledger: execute got pending transfer status " + status.String())
	case imported && stamp <= dr.Timestamp:
		return ErrImportedEventTimestampMustPostdateDebitAccount
	case imported && stamp <= cr.Timestamp:
		return ErrImportedEventTimestampMustPostdateCreditAccount
	// A closed account takes no movement but the void of a pending transfer.
	case isPost && dr.Flags.Has(AccountClosed):
		return ErrDebitAccountAlreadyClosed
	case isPost && cr.Flags.Has(AccountClosed):
		return ErrCreditAccountAlreadyClosed
	}
	// A closing transfer keeps its account closed until its void reopens it,
	// and only a void resolves it, as a post finds the account closed.
	reopenDr := isVoid && pend.Flags.Has(TransferClosingDebit)
	reopenCr := isVoid && pend.Flags.Has(TransferClosingCredit)
	if reopenDr && !dr.Flags.Has(AccountClosed) || reopenCr && !cr.Flags.Has(AccountClosed) {
		panic("ledger: execute of a void of a closing transfer whose account is open")
	}
	record := Transfer{
		ID:              submitted.ID,
		DebitAccountID:  pend.DebitAccountID,
		CreditAccountID: pend.CreditAccountID,
		PendingID:       pend.ID,
		Amount:          amount,
		UserData64:      cmp.Or(submitted.UserData64, pend.UserData64),
		UserData32:      cmp.Or(submitted.UserData32, pend.UserData32),
		Ledger:          pend.Ledger,
		Code:            pend.Code,
		Flags:           submitted.Flags,
		Timestamp:       stamp,
	}
	execAccounts(dr, cr, &record, pend.Amount)
	if reopenDr {
		dr.Flags &^= AccountClosed
	}
	if reopenCr {
		cr.Flags &^= AccountClosed
	}
	*submitted = record
	return 0
}

// executeCreate applies a plain or pending transfer, in create_transfer's order.
func executeCreate(dr, cr *Account, submitted *Transfer, stamp int64) ErrCode {
	imported := submitted.Flags.Has(TransferImported)
	switch {
	case dr == nil:
		return ErrDebitAccountNotFound
	case cr == nil:
		return ErrCreditAccountNotFound
	case dr.ID != submitted.DebitAccountID || cr.ID != submitted.CreditAccountID:
		panic("ledger: execute got accounts other than the ones the transfer names")
	case dr.Ledger != cr.Ledger:
		return ErrAccountsMustHaveTheSameLedger
	case submitted.Ledger != dr.Ledger:
		return ErrTransferMustHaveTheSameLedgerAsAccounts
	case imported && stamp <= dr.Timestamp:
		return ErrImportedEventTimestampMustPostdateDebitAccount
	case imported && stamp <= cr.Timestamp:
		return ErrImportedEventTimestampMustPostdateCreditAccount
	case dr.Flags.Has(AccountClosed):
		return ErrDebitAccountAlreadyClosed
	case cr.Flags.Has(AccountClosed):
		return ErrCreditAccountAlreadyClosed
	}

	// Balancing moves at most what keeps the account within its posted opposite side,
	// whatever its flags, and may move zero.
	drTotal := sum64(dr.DebitsPending, dr.DebitsPosted)
	crTotal := sum64(cr.CreditsPending, cr.CreditsPosted)
	amount := submitted.Amount
	if submitted.Flags.Has(TransferBalancingDebit) {
		amount = min(amount, room(dr.CreditsPosted, drTotal))
	}
	if submitted.Flags.Has(TransferBalancingCredit) {
		amount = min(amount, room(cr.DebitsPosted, crTotal))
	}
	isPending := submitted.IsPending()
	switch {
	case isPending && !fits(dr.DebitsPending, amount):
		return ErrOverflowsDebitsPending
	case isPending && !fits(cr.CreditsPending, amount):
		return ErrOverflowsCreditsPending
	case !fits(dr.DebitsPosted, amount):
		return ErrOverflowsDebitsPosted
	case !fits(cr.CreditsPosted, amount):
		return ErrOverflowsCreditsPosted
	case !fits128(drTotal, amount):
		return ErrOverflowsDebits
	case !fits128(crTotal, amount):
		return ErrOverflowsCredits
	// The totals fit with amount added, so drTotal.lo+amount is exact below.
	case dr.Flags.Has(AccountDebitsMustNotExceedCredits) && drTotal.lo+amount > dr.CreditsPosted:
		return ErrExceedsCredits
	case cr.Flags.Has(AccountCreditsMustNotExceedDebits) && crTotal.lo+amount > cr.DebitsPosted:
		return ErrExceedsDebits
	}
	if !isPending && submitted.Flags.Has(TransferClosingDebit|TransferClosingCredit) {
		panic("ledger: execute of a closing transfer that is not pending") // Validate rejects it
	}
	rec := *submitted
	rec.Amount = amount
	rec.Timestamp = stamp
	execAccounts(dr, cr, &rec, 0)
	// Closed accounts were rejected above, so a closing transfer finds its account open.
	if rec.Flags.Has(TransferClosingDebit) {
		dr.Flags |= AccountClosed
	}
	if rec.Flags.Has(TransferClosingCredit) {
		cr.Flags |= AccountClosed
	}
	*submitted = rec
	return 0
}

// execAccounts applies record to the counters of its accounts dr and cr. reserved is the
// amount of the pending transfer record posts or voids, zero if none. Callers validate
// record first, after which it must apply: a failure means the counters are inconsistent
// with the records, so it panics, leaving dr and cr unchanged.
func execAccounts(dr, cr *Account, record *Transfer, reserved uint64) {
	// dr is applied on a copy so a failure on cr leaves neither changed.
	dpend, dpost := dr.DebitsPending, dr.DebitsPosted
	if exec(&dpend, &dpost, record, reserved, true) != 0 ||
		exec(&cr.CreditsPending, &cr.CreditsPosted, record, reserved, false) != 0 {
		panic("ledger: execute of a validated transfer failed on the account counters")
	}
	dr.DebitsPending, dr.DebitsPosted = dpend, dpost
}

// exec applies accepted to the pending and posted counters of one side of an account,
// the debit side if debit, which picks the codes returned. reserved is the amount of the
// pending transfer accepted posts or voids, zero if none; accepted.Amount must not exceed
// it. A nonzero code leaves the counters unchanged: an overflow of the named counter, or
// ErrPendingTransferNotFound if the counters do not hold the reservation.
func exec(pending, posted *uint64, accepted *Transfer, reserved uint64, debit bool) ErrCode {
	pd, ps := *pending, *posted
	pdOK, psOK := true, true
	switch {
	case accepted.IsPending():
		pd, pdOK = add64(pd, accepted.Amount)
	case accepted.IsResolving():
		if accepted.Amount > reserved {
			panic("ledger: execute of a post or void above its reservation")
		}
		pd, pdOK = sub64(pd, reserved) // a post releases the rest it does not commit
		if pdOK && accepted.Flags.Has(TransferPostPending) {
			ps, psOK = add64(ps, accepted.Amount)
		}
	default:
		ps, psOK = add64(ps, accepted.Amount)
	}
	switch {
	case !pdOK && accepted.IsResolving():
		return ErrPendingTransferNotFound
	case !pdOK:
		return pick(debit, ErrOverflowsDebitsPending, ErrOverflowsCreditsPending)
	case !psOK:
		return pick(debit, ErrOverflowsDebitsPosted, ErrOverflowsCreditsPosted)
	}
	*pending, *posted = pd, ps
	return 0
}

// pick returns dr if debit, else cr.
func pick[T any](debit bool, dr, cr T) T {
	if debit {
		return dr
	}
	return cr
}

func firstNzMismatch[T ~uint16 | ~uint32 | ~uint64](first, second T) bool {
	return first != 0 && first != second
}

func add64(x, y uint64) (uint64, bool) {
	s, carry := bits.Add64(x, y, 0)
	return s, carry == 0
}

func sub64(x, y uint64) (uint64, bool) {
	d, borrow := bits.Sub64(x, y, 0)
	return d, borrow == 0
}

// fits reports whether counter+amount fits a uint64.
func fits(counter, amount uint64) bool {
	_, ok := add64(counter, amount)
	return ok
}

// fits128 reports whether total+amount fits a uint64.
func fits128(total u128, amount uint64) bool {
	return total.hi == 0 && fits(total.lo, amount)
}

// room returns limit-used, or zero if used reaches limit: TigerBeetle's saturating
// subtraction for balancing transfers.
func room(limit uint64, used u128) uint64 {
	if used.hi != 0 || used.lo >= limit {
		return 0
	}
	return limit - used.lo
}

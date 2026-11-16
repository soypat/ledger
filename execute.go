package ledger

import (
	"cmp"
	"math/bits"
)

// ExecuteOnBalance adds accepted transfer on account id to b;
func ExecuteReplay(acc *Account, accepted, pending *Transfer) (code ErrCode) {
	debitor, creditor := accepted.DebitAccountID, accepted.CreditAccountID
	var aux Transfer
	var reserved uint64
	if accepted.IsResolving() {
		if pending == nil || pending.ID != accepted.ID {
			panic("execute on balance needs the pending transfer posted resolves")
		}
		// accepted may leave them to inherit.
		debitor, creditor = pending.DebitAccountID, pending.CreditAccountID
		reserved = pending.Amount
		if accepted.Flags.Has(TransferPostPending) && accepted.Amount == AmountMax {
			aux = *accepted
			aux.Amount = pending.Amount // AmountMax posts whole reservation.
			accepted = &aux
		}
	} else if pending != nil {
		panic("execute on balance got pending transfer for one that resolves none")
	}
	switch acc.ID {
	case debitor:
		code = exec(&acc.DebitsPending, &acc.DebitsPosted, accepted, reserved)
	case creditor:
		code = exec(&acc.CreditsPending, &acc.CreditsPosted, accepted, reserved)
	default:
		code = ErrCreditAccountNotFound
	}
	return code
}

// Execute validates submitted transfer and, if rules allow it, applies it to counters of accounts dr and cr.
// It returns zero ErrCode on success, else first broken rule in order of precedence.
func Execute(submitted *Transfer, ts int64, dr, cr *Account, pending *Transfer, status PendingStatus) ErrCode {
	if code := submitted.Validate(); code != 0 {
		return code
	}
	stamp := submitted.Timestamp
	if submitted.Flags.Has(TransferImported) {
		if ts <= 0 {
			panic("execute on non positive timestamp")
		}
		stamp = ts
	}
	if submitted.IsResolving() {
		return executeResolve(submitted, stamp, dr, cr, pending, status)
	}
	if pending != nil {
		panic("execute got pending transfer for a posted that resolves none")
	}
	return executeCreate(submitted, stamp, dr, cr)
}

// executeResolve applies a submitted or void of pending, in post_or_void_pending_transfer's order.
func executeResolve(submitted *Transfer, stamp int64, dr, cr *Account, pend *Transfer, status PendingStatus) (code ErrCode) {
	// A post of AmountMax posts it all; a void of zero voids it all. Otherwise exact amount.
	var isPost = submitted.Flags.Has(TransferPostPending)
	var isVoid = submitted.Flags.Has(TransferVoidPending)
	amount := submitted.Amount
	if (isPost && amount == AmountMax) || (!isPost && amount == 0) {
		amount = pend.Amount
	}
	imported := submitted.Flags.Has(TransferImported)
	switch {
	case pend == nil:
		code = ErrPendingTransferNotFound
	case pend.ID != submitted.ID:
		panic("execute pending transfer other than the one posted names")
	case !pend.IsPending():
		code = ErrPendingTransferNotPending
	case firstNzMismatch(submitted.DebitAccountID, pend.DebitAccountID):
		code = ErrPendingTransferHasDifferentDebitAccountID
	case firstNzMismatch(submitted.CreditAccountID, pend.CreditAccountID):
		code = ErrPendingTransferHasDifferentCreditAccountID
	case firstNzMismatch(submitted.Ledger, pend.Ledger):
		code = ErrPendingTransferHasDifferentLedger
	case firstNzMismatch(submitted.Code, pend.Code):
		code = ErrPendingTransferHasDifferentCode
	case amount > submitted.Amount:
		code = ErrExceedsPendingTransferAmount
	case !isPost && amount < pend.Amount:
		code = ErrPendingTransferHasDifferentAmount

	// case status == PendingActive:
	case status == PendingPosted:
		code = ErrPendingTransferAlreadyPosted
	case status == PendingVoided:
		code = ErrPendingTransferAlreadyVoided
	case status == PendingNone || status >= _pendingMax:
		panic("execute got pending transfer status " + status.String())
	case dr == nil || cr == nil || dr.ID != pend.DebitAccountID || cr.ID != pend.CreditAccountID:
		panic("execute got accounts other than pending transfer's")
	case isPost == isVoid:
		panic("execute either flags post or void")
	case imported && submitted.Timestamp <= dr.Timestamp:
		code = ErrImportedEventTimestampMustPostdateDebitAccount
	case imported && submitted.Timestamp <= cr.Timestamp:
		code = ErrImportedEventTimestampMustPostdateCreditAccount
	// A closed account takes no movement but the void of a pending stransfer.
	case isPost && dr.Flags.Has(AccountClosed):
		code = ErrDebitAccountAlreadyClosed
	case isPost && cr.Flags.Has(AccountClosed):
		code = ErrCreditAccountAlreadyClosed
	}
	if code != 0 {
		return code
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
	code = execAccounts(dr, cr, &record, pend.Amount)
	if code != 0 {
		return code
	}
	if isVoid {
		if pend.Flags.Has(TransferClosingDebit) {
			dr.Flags &^= AccountClosed // voiding a closing transfer reopens the account
		}
		if pend.Flags.Has(TransferClosingDebit) {
			cr.Flags &^= AccountClosed
		}
	}
	*submitted = record
	return code
}

func executeCreate(submitted *Transfer, stamp int64, dr, cr *Account) (code ErrCode) {
	imported := submitted.Flags.Has(TransferImported)
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
	case dr == nil:
		code = ErrDebitAccountNotFound
	case cr == nil:
		code = ErrCreditAccountNotFound
	case dr.ID != submitted.DebitAccountID || cr.ID != submitted.CreditAccountID:
		panic("execute got accounts other than the ones transfer names")
	case dr.Ledger != cr.Ledger:
		code = ErrAccountsMustHaveTheSameLedger
	case submitted.Ledger != dr.Ledger:
		code = ErrTransferMustHaveTheSameLedgerAsAccounts
	case imported && submitted.Timestamp <= dr.Timestamp:
		code = ErrImportedEventTimestampMustPostdateDebitAccount
	case imported && submitted.Timestamp <= cr.Timestamp:
		code = ErrImportedEventTimestampMustPostdateCreditAccount
	case dr.Flags.Has(AccountClosed):
		code = ErrDebitAccountAlreadyClosed
	case cr.Flags.Has(AccountClosed):
		code = ErrCreditAccountAlreadyClosed

	// Amount validation.
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
	rec := *submitted
	rec.Amount = amount
	rec.Timestamp = stamp
	code = execAccounts(dr, cr, &rec, 0)
	if code != 0 {
		return code
	}
	if isPending {
		if submitted.Flags.Has(TransferClosingDebit) {
			dr.Flags |= AccountClosed
		}
		if submitted.Flags.Has(TransferClosingCredit) {
			cr.Flags |= AccountClosed
		}
	}
	*submitted = rec
	return 0
}

const (
	errExecOverflow = ErrOverflowsCredits
	errExecReserved = ErrReservedFlag
)

// execAccounts applies record transfer to counters of its accounts dr and cr.
// reserved is amount pending transfer t posts or voids, zero if none. Does not modify accounts on overflow/underflow or commits more than reserved.
func execAccounts(dr, cr *Account, accepted *Transfer, reserved uint64) ErrCode {
	// Will run dr first on copy so we don't modify it and fail on cr leaving inconsistent state.
	dpend, dpost := dr.DebitsPending, dr.DebitsPosted
	if code := exec(&dpend, &dpost, accepted, reserved); code != 0 {
		// func to specialize error?
		return code
	}
	if code := exec(&cr.CreditsPending, &cr.CreditsPosted, accepted, reserved); code != 0 {
		return code
	}
	dr.DebitsPending, dr.DebitsPosted = dpend, dpost
	return 0
}

// exec adds accepted transfer t.Amount effect on one side of an account given the
// account's pending and posted counters and updates these. exec returns true if applied succesfully;
// false if counters left unchanged due to overflow/undeflow or if a post commits more than reserved.
func exec(pending, posted *uint64, accepted *Transfer, reserved uint64) (code ErrCode) {
	pd, ps := *pending, *posted
	var ok bool
	switch {
	case accepted.IsPending():
		pd, ok = add64(pd, accepted.Amount)
	case accepted.Flags.Has(TransferPostPending):
		if accepted.Amount > reserved {
			return errExecReserved
		}
		var released, committed bool
		pd, released = sub64(pd, reserved) // the unposted rest is released
		ps, committed = add64(ps, accepted.Amount)
		ok = released && committed
	case accepted.Flags.Has(TransferVoidPending):
		pd, ok = sub64(pd, reserved)
	default:
		ps, ok = add64(ps, accepted.Amount)
	}
	if ok {
		*pending, *posted = pd, ps
	} else {
		code = errExecOverflow
	}
	return code
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

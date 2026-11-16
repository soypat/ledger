package ledger

import (
	"cmp"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

const (
	execTS  = 1000 // timestamp the ledger assigns in these tests
	idDebit = 10
	idCred  = 20
)

// accounts returns an open debit and credit account on ledger 7, matching newBuilder.
func accounts() (dr, cr Account) {
	return Account{ID: idDebit, Ledger: 7, Code: 1, Timestamp: 100},
		Account{ID: idCred, Ledger: 7, Code: 1, Timestamp: 200}
}

type ExecuteTest struct {
	name      string
	dr, cr    Account
	submitted Transfer
	pending   *Transfer
	status    PendingStatus // zero derives PendingActive when pending is a pending transfer
	ts        int64         // ledger clock; zero derives one past every timestamp in the row
	// Outcome.
	wantCode ErrCode // No panic test here.
}

// The txWith helpers and acctWithLedger return a fixture with one thing different.
// A zero account ID in txWithAccounts leaves a post or void to inherit it.

func txWithLedger(tr Transfer, ledger uint32) Transfer { tr.Ledger = ledger; return tr }

func txWithCode(tr Transfer, code uint16) Transfer { tr.Code = code; return tr }

func txWithFlags(tr Transfer, flags TransferFlags) Transfer { tr.Flags = flags; return tr }

func txWithAccounts(tr Transfer, debit, credit ID) Transfer {
	tr.DebitAccountID, tr.CreditAccountID = debit, credit
	return tr
}

func txWithUserData(tr Transfer, ud64 uint64, ud32 uint32) Transfer {
	tr.UserData64, tr.UserData32 = ud64, ud32
	return tr
}

func acctWithLedger(a Account, ledger uint32) Account { a.Ledger = ledger; return a }

func TestExecute_table(t *testing.T) {
	const (
		ledger = 7
		idDeb  = 2
		idCred = 3
		idSub  = 4 // ID of the submitted transfer
		idPend = 5 // ID of the pending transfer that posts and voids resolve
		u64max = math.MaxUint64

		closed  = AccountClosed
		drLimit = AccountDebitsMustNotExceedCredits
		crLimit = AccountCreditsMustNotExceedDebits
	)
	// B returns a balance from its four counters, in Balance's field order.
	B := func(debitsPending, debitsPosted, creditsPending, creditsPosted uint64) Balance {
		return Balance{
			DebitsPending:  debitsPending,
			DebitsPosted:   debitsPosted,
			CreditsPending: creditsPending,
			CreditsPosted:  creditsPosted,
		}
	}
	account := func(id ID, ts int64, b Balance, flags AccountFlags) Account {
		return Account{
			ID:        id,
			Ledger:    ledger,
			Code:      1,
			Balance:   b,
			Flags:     flags,
			Timestamp: ts,
		}
	}
	// debitor and creditor return the two accounts of a row, created at 100 and 200.
	debitor := func(b Balance, flags AccountFlags) Account { return account(idDeb, 100, b, flags) }
	creditor := func(b Balance, flags AccountFlags) Account { return account(idCred, 200, b, flags) }
	// sub returns a transfer of amount from debitor to creditor, submitted with flags.
	sub := func(amount uint64, flags TransferFlags) Transfer {
		return Transfer{
			ID:              idSub,
			DebitAccountID:  idDeb,
			CreditAccountID: idCred,
			Amount:          amount,
			Ledger:          ledger,
			Code:            1,
			Flags:           flags,
		}
	}
	// post and void return a post or void of pend that names only pend, inheriting the rest.
	post := func(amount uint64) Transfer {
		return Transfer{ID: idSub, PendingID: idPend, Amount: amount, Flags: TransferPostPending}
	}
	void := func(amount uint64) Transfer {
		return Transfer{ID: idSub, PendingID: idPend, Amount: amount, Flags: TransferVoidPending}
	}
	// imported returns tr submitted as imported, carrying its own timestamp ts.
	imported := func(tr Transfer, ts int64) Transfer {
		tr.Flags |= TransferImported
		tr.Timestamp = ts
		return tr
	}

	var (
		// Open and empty accounts; cr8 is on another ledger.
		dr0, cr0 = debitor(Balance{}, 0), creditor(Balance{}, 0)
		cr8      = acctWithLedger(cr0, 8)
		// pend reserves 50 from debitor to creditor, stored at 300, with UserData to inherit.
		pend = Transfer{ID: idPend, DebitAccountID: idDeb, CreditAccountID: idCred, Amount: 50,
			UserData64: 77, UserData32: 88, Ledger: ledger, Code: 1, Flags: TransferPending, Timestamp: 300}
		pendClosingDr = txWithFlags(pend, TransferPending|TransferClosingDebit)
		pendClosingCr = txWithFlags(pend, TransferPending|TransferClosingCredit)
		notPending    = txWithFlags(pend, 0)
		// Accounts holding pend's reservation, open and closed.
		drHeld, crHeld             = debitor(B(50, 0, 0, 0), 0), creditor(B(0, 0, 50, 0), 0)
		drHeldClosed, crHeldClosed = debitor(B(50, 0, 0, 0), closed), creditor(B(0, 0, 50, 0), closed)
	)

	var tests = []ExecuteTest{
		// Plain and pending.
		{name: "plain", dr: dr0, cr: cr0, submitted: sub(100, 0)},
		{name: "plain adds to existing counters", dr: debitor(B(1, 2, 3, 4), 0), cr: creditor(B(5, 6, 7, 8), 0), submitted: sub(100, 0)},
		{name: "pending reserves", dr: dr0, cr: cr0, submitted: sub(100, TransferPending)},
		{name: "pending adds to existing counters", dr: debitor(B(1, 2, 3, 4), 0), cr: creditor(B(5, 6, 7, 8), 0), submitted: sub(100, TransferPending)},
		{name: "zero amount moves nothing", dr: dr0, cr: cr0, submitted: sub(0, 0)},
		{name: "largest amount fits empty accounts", dr: dr0, cr: cr0, submitted: sub(u64max, 0)},
		{name: "exactly fills debits posted", dr: debitor(B(0, u64max-100, 0, 0), 0), cr: cr0, submitted: sub(100, 0)},
		{name: "imported just after creditor keeps its timestamp", dr: dr0, cr: cr0, submitted: imported(sub(100, 0), 201)},

		// Balancing: moves at most the room the account has, whatever its flags.
		{name: "balancing debit takes the room left", dr: debitor(B(0, 30, 0, 100), drLimit), cr: cr0, submitted: sub(AmountMax, TransferBalancingDebit)},
		{name: "balancing debit below the room moves it all", dr: debitor(B(0, 30, 0, 100), drLimit), cr: cr0, submitted: sub(5, TransferBalancingDebit)},
		{name: "balancing debit counts pending debits", dr: debitor(B(20, 30, 0, 100), drLimit), cr: cr0, submitted: sub(AmountMax, TransferBalancingDebit)},
		{name: "balancing debit without room moves zero", dr: debitor(B(0, 100, 0, 100), drLimit), cr: cr0, submitted: sub(5, TransferBalancingDebit)},
		{name: "balancing debit clamps without the account flag", dr: dr0, cr: cr0, submitted: sub(5, TransferBalancingDebit)},
		{name: "balancing credit takes the room left", dr: dr0, cr: creditor(B(0, 1000, 0, 70), crLimit), submitted: sub(AmountMax, TransferBalancingCredit)},
		{name: "pending balancing credit reserves the room left", dr: dr0, cr: creditor(B(0, 1000, 0, 70), crLimit), submitted: sub(AmountMax, TransferBalancingCredit|TransferPending)},
		{name: "balancing both takes the smaller room", dr: debitor(B(0, 30, 0, 100), drLimit), cr: creditor(B(0, 40, 0, 0), 0), submitted: sub(AmountMax, TransferBalancingDebit|TransferBalancingCredit)},

		// Account limits at their edge.
		{name: "exactly reaches credits", dr: debitor(B(0, 0, 0, 100), drLimit), cr: cr0, submitted: sub(100, 0)},
		{name: "exactly reaches debits", dr: dr0, cr: creditor(B(0, 100, 0, 0), crLimit), submitted: sub(100, 0)},

		// Post and void of pend.
		{name: "post of part releases the rest", dr: drHeld, cr: crHeld, pending: &pend, submitted: post(30)},
		{name: "post of the exact amount", dr: drHeld, cr: crHeld, pending: &pend, submitted: post(50)},
		{name: "post of AmountMax posts all", dr: drHeld, cr: crHeld, pending: &pend, submitted: post(AmountMax)},
		{name: "post of zero releases all", dr: drHeld, cr: crHeld, pending: &pend, submitted: post(0)},
		{name: "void of zero voids all", dr: drHeld, cr: crHeld, pending: &pend, submitted: void(0)},
		{name: "void of the exact amount", dr: drHeld, cr: crHeld, pending: &pend, submitted: void(50)},
		{name: "post among other reservations", dr: debitor(B(80, 10, 0, 0), 0), cr: creditor(B(0, 0, 80, 10), 0), pending: &pend, submitted: post(30)},
		{name: "post keeps its own user data", dr: drHeld, cr: crHeld, pending: &pend, submitted: txWithUserData(post(AmountMax), 5, 6)},
		{name: "post naming its pending's accounts", dr: drHeld, cr: crHeld, pending: &pend, submitted: txWithAccounts(post(AmountMax), idDeb, idCred)},
		{name: "post naming its pending's ledger and code", dr: drHeld, cr: crHeld, pending: &pend, submitted: txWithCode(txWithLedger(post(AmountMax), ledger), 1)},
		{name: "imported post keeps its timestamp", dr: drHeld, cr: crHeld, pending: &pend, submitted: imported(post(AmountMax), 400)},

		// Closing: a pending closing transfer closes its side until its void.
		{name: "closing debit closes the debitor", dr: dr0, cr: cr0, submitted: sub(100, TransferPending|TransferClosingDebit)},
		{name: "closing credit closes the creditor", dr: dr0, cr: cr0, submitted: sub(100, TransferPending|TransferClosingCredit)},
		{name: "closing both closes both", dr: dr0, cr: cr0, submitted: sub(100, TransferPending|TransferClosingDebit|TransferClosingCredit)},
		{name: "void of closing debit reopens the debitor", dr: drHeldClosed, cr: crHeld, pending: &pendClosingDr, submitted: void(0)},
		{name: "void of closing credit reopens the creditor", dr: drHeld, cr: crHeldClosed, pending: &pendClosingCr, submitted: void(0)},
		{name: "void of closing debit leaves a closed creditor closed", dr: drHeldClosed, cr: crHeldClosed, pending: &pendClosingDr, submitted: void(0)},
		{name: "void of closing credit leaves a closed debitor closed", dr: drHeldClosed, cr: crHeldClosed, pending: &pendClosingCr, submitted: void(0)},
		{name: "void of another pending keeps the debitor closed", dr: drHeldClosed, cr: crHeld, pending: &pend, submitted: void(0)},
		{name: "void of another pending keeps the creditor closed", dr: drHeld, cr: crHeldClosed, pending: &pend, submitted: void(0)},

		// Rejected: validation, ledgers, imported timestamps, closed accounts.
		{name: "invalid transfer", dr: dr0, cr: cr0, submitted: txWithLedger(sub(100, 0), 0), wantCode: ErrLedgerMustNotBeZero},
		{name: "accounts on different ledgers", dr: dr0, cr: cr8, submitted: sub(100, 0), wantCode: ErrAccountsMustHaveTheSameLedger},
		{name: "transfer on another ledger", dr: dr0, cr: cr0, submitted: txWithLedger(sub(100, 0), 8), wantCode: ErrTransferMustHaveTheSameLedgerAsAccounts},
		{name: "imported not after debitor", dr: dr0, cr: cr0, submitted: imported(sub(100, 0), 100), wantCode: ErrImportedEventTimestampMustPostdateDebitAccount},
		{name: "imported not after creditor", dr: dr0, cr: cr0, submitted: imported(sub(100, 0), 200), wantCode: ErrImportedEventTimestampMustPostdateCreditAccount},
		{name: "debitor closed", dr: debitor(Balance{}, closed), cr: cr0, submitted: sub(100, 0), wantCode: ErrDebitAccountAlreadyClosed},
		{name: "creditor closed", dr: dr0, cr: creditor(Balance{}, closed), submitted: sub(100, 0), wantCode: ErrCreditAccountAlreadyClosed},

		// Rejected: overflows.
		{name: "overflows debits pending", dr: debitor(B(u64max-10, 0, 0, 0), 0), cr: cr0, submitted: sub(100, TransferPending), wantCode: ErrOverflowsDebitsPending},
		{name: "overflows credits pending", dr: dr0, cr: creditor(B(0, 0, u64max-10, 0), 0), submitted: sub(100, TransferPending), wantCode: ErrOverflowsCreditsPending},
		{name: "overflows debits posted", dr: debitor(B(0, u64max-10, 0, 0), 0), cr: cr0, submitted: sub(100, 0), wantCode: ErrOverflowsDebitsPosted},
		{name: "overflows credits posted", dr: dr0, cr: creditor(B(0, 0, 0, u64max-10), 0), submitted: sub(100, 0), wantCode: ErrOverflowsCreditsPosted},
		{name: "pending checks posted overflow too", dr: debitor(B(0, u64max-10, 0, 0), 0), cr: cr0, submitted: sub(100, TransferPending), wantCode: ErrOverflowsDebitsPosted},
		{name: "overflows debits", dr: debitor(B(u64max/2+1, u64max/2, 0, 0), 0), cr: cr0, submitted: sub(100, 0), wantCode: ErrOverflowsDebits},
		{name: "overflows credits", dr: dr0, cr: creditor(B(0, 0, u64max/2+1, u64max/2), 0), submitted: sub(100, 0), wantCode: ErrOverflowsCredits},

		// Rejected: account limits.
		{name: "exceeds credits", dr: debitor(B(0, 0, 0, 99), drLimit), cr: cr0, submitted: sub(100, 0), wantCode: ErrExceedsCredits},
		{name: "exceeds credits counts pending debits", dr: debitor(B(60, 0, 0, 150), drLimit), cr: cr0, submitted: sub(100, 0), wantCode: ErrExceedsCredits},
		{name: "exceeds credits ignores pending credits", dr: debitor(B(0, 0, 1000, 0), drLimit), cr: cr0, submitted: sub(100, 0), wantCode: ErrExceedsCredits},
		{name: "exceeds debits", dr: dr0, cr: creditor(B(0, 99, 0, 0), crLimit), submitted: sub(100, 0), wantCode: ErrExceedsDebits},
		{name: "exceeds debits counts pending credits", dr: dr0, cr: creditor(B(0, 150, 60, 0), crLimit), submitted: sub(100, 0), wantCode: ErrExceedsDebits},
		{name: "exceeds debits ignores pending debits", dr: dr0, cr: creditor(B(1000, 0, 0, 0), crLimit), submitted: sub(100, 0), wantCode: ErrExceedsDebits},

		// Rejected: post and void of pend.
		{name: "pending transfer not found", dr: drHeld, cr: crHeld, submitted: post(AmountMax), wantCode: ErrPendingTransferNotFound},
		{name: "pending transfer not pending", dr: drHeld, cr: crHeld, pending: &notPending, submitted: post(AmountMax), wantCode: ErrPendingTransferNotPending},
		{name: "different debit account", dr: drHeld, cr: crHeld, pending: &pend, submitted: txWithAccounts(post(AmountMax), 99, 0), wantCode: ErrPendingTransferHasDifferentDebitAccountID},
		{name: "different credit account", dr: drHeld, cr: crHeld, pending: &pend, submitted: txWithAccounts(post(AmountMax), 0, 99), wantCode: ErrPendingTransferHasDifferentCreditAccountID},
		{name: "equal accounts other than pending's", dr: drHeld, cr: crHeld, pending: &pend, submitted: txWithAccounts(post(AmountMax), idDeb, idDeb), wantCode: ErrPendingTransferHasDifferentCreditAccountID},
		{name: "different ledger", dr: drHeld, cr: crHeld, pending: &pend, submitted: txWithLedger(post(AmountMax), 8), wantCode: ErrPendingTransferHasDifferentLedger},
		{name: "different code", dr: drHeld, cr: crHeld, pending: &pend, submitted: txWithCode(post(AmountMax), 9), wantCode: ErrPendingTransferHasDifferentCode},
		{name: "post above the reservation", dr: drHeld, cr: crHeld, pending: &pend, submitted: post(51), wantCode: ErrExceedsPendingTransferAmount},
		{name: "void above the reservation", dr: drHeld, cr: crHeld, pending: &pend, submitted: void(51), wantCode: ErrExceedsPendingTransferAmount},
		{name: "void below the reservation", dr: drHeld, cr: crHeld, pending: &pend, submitted: void(49), wantCode: ErrPendingTransferHasDifferentAmount},
		{name: "post of a posted", dr: drHeld, cr: crHeld, pending: &pend, status: PendingPosted, submitted: post(AmountMax), wantCode: ErrPendingTransferAlreadyPosted},
		{name: "void of a posted", dr: drHeld, cr: crHeld, pending: &pend, status: PendingPosted, submitted: void(0), wantCode: ErrPendingTransferAlreadyPosted},
		{name: "post of a voided", dr: drHeld, cr: crHeld, pending: &pend, status: PendingVoided, submitted: post(AmountMax), wantCode: ErrPendingTransferAlreadyVoided},
		{name: "void of a voided", dr: drHeld, cr: crHeld, pending: &pend, status: PendingVoided, submitted: void(0), wantCode: ErrPendingTransferAlreadyVoided},
		{name: "imported post not after debitor", dr: drHeld, cr: crHeld, pending: &pend, submitted: imported(post(AmountMax), 100), wantCode: ErrImportedEventTimestampMustPostdateDebitAccount},
		{name: "imported post not after creditor", dr: drHeld, cr: crHeld, pending: &pend, submitted: imported(post(AmountMax), 200), wantCode: ErrImportedEventTimestampMustPostdateCreditAccount},
		{name: "post on closed debitor", dr: drHeldClosed, cr: crHeld, pending: &pend, submitted: post(AmountMax), wantCode: ErrDebitAccountAlreadyClosed},
		{name: "post on closed creditor", dr: drHeld, cr: crHeldClosed, pending: &pend, submitted: post(AmountMax), wantCode: ErrCreditAccountAlreadyClosed},
		{name: "post of a closing transfer", dr: drHeldClosed, cr: crHeld, pending: &pendClosingDr, submitted: post(AmountMax), wantCode: ErrDebitAccountAlreadyClosed},

		// Precedence: two rules broken, TigerBeetle's order picks the code.
		{name: "validation before ledgers", dr: dr0, cr: cr8, submitted: txWithLedger(sub(100, 0), 0), wantCode: ErrLedgerMustNotBeZero},
		{name: "accounts' ledgers before the transfer's", dr: dr0, cr: cr8, submitted: txWithLedger(sub(100, 0), 9), wantCode: ErrAccountsMustHaveTheSameLedger},
		{name: "ledgers before closed", dr: debitor(Balance{}, closed), cr: cr8, submitted: sub(100, 0), wantCode: ErrAccountsMustHaveTheSameLedger},
		{name: "imported timestamp before closed", dr: debitor(Balance{}, closed), cr: cr0, submitted: imported(sub(100, 0), 100), wantCode: ErrImportedEventTimestampMustPostdateDebitAccount},
		{name: "debitor closed before creditor closed", dr: debitor(Balance{}, closed), cr: creditor(Balance{}, closed), submitted: sub(100, 0), wantCode: ErrDebitAccountAlreadyClosed},
		{name: "closed before overflow", dr: debitor(B(0, u64max, 0, 0), closed), cr: cr0, submitted: sub(100, 0), wantCode: ErrDebitAccountAlreadyClosed},
		{name: "pending overflow before posted", dr: debitor(B(u64max-10, u64max-10, 0, 0), 0), cr: cr0, submitted: sub(100, TransferPending), wantCode: ErrOverflowsDebitsPending},
		{name: "debits pending before credits pending", dr: debitor(B(u64max-10, 0, 0, 0), 0), cr: creditor(B(0, 0, u64max-10, 0), 0), submitted: sub(100, TransferPending), wantCode: ErrOverflowsDebitsPending},
		{name: "overflow before exceeds", dr: debitor(B(0, u64max-10, 0, 0), drLimit), cr: cr0, submitted: sub(100, 0), wantCode: ErrOverflowsDebitsPosted},
		{name: "exceeds credits before exceeds debits", dr: debitor(Balance{}, drLimit), cr: creditor(Balance{}, crLimit), submitted: sub(100, 0), wantCode: ErrExceedsCredits},
		{name: "not pending before different account", dr: drHeld, cr: crHeld, pending: &notPending, submitted: txWithAccounts(post(AmountMax), 99, 0), wantCode: ErrPendingTransferNotPending},
		{name: "different account before amount", dr: drHeld, cr: crHeld, pending: &pend, submitted: txWithAccounts(post(51), 99, 0), wantCode: ErrPendingTransferHasDifferentDebitAccountID},
		{name: "amount before status", dr: drHeld, cr: crHeld, pending: &pend, status: PendingPosted, submitted: post(51), wantCode: ErrExceedsPendingTransferAmount},
		{name: "status before imported timestamp", dr: drHeld, cr: crHeld, pending: &pend, status: PendingPosted, submitted: imported(post(AmountMax), 100), wantCode: ErrPendingTransferAlreadyPosted},
		{name: "status before closed", dr: drHeldClosed, cr: crHeld, pending: &pend, status: PendingPosted, submitted: post(AmountMax), wantCode: ErrPendingTransferAlreadyPosted},
		{name: "imported timestamp before closed on post", dr: drHeldClosed, cr: crHeld, pending: &pend, submitted: imported(post(AmountMax), 100), wantCode: ErrImportedEventTimestampMustPostdateDebitAccount},
	}
	for i := range tests {
		test := &tests[i]
		t.Run(test.name, func(t *testing.T) {
			testExecuteNoPanic(t, test)
		})
	}
}

func testExecuteNoPanic(t *testing.T, test *ExecuteTest) {
	dr := test.dr
	cr := test.cr
	sub := test.submitted
	var auxpend Transfer
	var pend *Transfer
	if test.pending != nil {
		auxpend = *test.pending
		pend = &auxpend
	}
	ts := test.ts
	if ts == 0 {
		// The ledger clock is past every stored record and any imported timestamp.
		ts = 1 + max(dr.Timestamp, cr.Timestamp, sub.Timestamp)
		if pend != nil {
			ts = max(ts, pend.Timestamp+1)
		}
	}
	status := test.status
	if status == PendingNone && pend != nil && pend.IsPending() {
		status = PendingActive // a pending transfer not yet resolved
	}
	code := Execute(&dr, &cr, &sub, pend, status, ts)
	if code != test.wantCode {
		t.Errorf("want code %s, got %s", test.wantCode, code)
		return
	} else if code != 0 {
		// Failed, check immutable
		if dr != test.dr || cr != test.cr {
			t.Error("on error immutable account promise broken")
		}
		if test.submitted != sub {
			t.Error("on error immutable submitted promise broken")
		}
		if test.pending != nil && auxpend != *test.pending {
			t.Error("on error immutable pending promise broken")
		}
		return
	}
	// Behavioural testing of values: on success everything follows from the row.
	in, rec := test.submitted, sub // as submitted, as stored
	isPending := in.IsPending()
	isPost := in.Flags.Has(TransferPostPending)
	isVoid := in.Flags.Has(TransferVoidPending)

	// Amount is the only value Execute chooses.
	wantAmount := in.Amount
	if isVoid || isPost && in.Amount == AmountMax {
		wantAmount = pend.Amount // a void releases it all; AmountMax posts it all
	}
	if in.Flags.Has(TransferBalancingDebit) {
		room, ok := test.dr.DebitHeadroom()
		if !ok {
			t.Fatal("row: debit headroom does not fit an int64")
		}
		wantAmount = min(wantAmount, uint64(max(room, 0)))
	}
	if in.Flags.Has(TransferBalancingCredit) {
		room, ok := test.cr.CreditHeadroom()
		if !ok {
			t.Fatal("row: credit headroom does not fit an int64")
		}
		wantAmount = min(wantAmount, uint64(max(room, 0)))
	}

	// Stored record.
	wantRec := in
	wantRec.Amount = wantAmount
	if !in.Flags.Has(TransferImported) {
		wantRec.Timestamp = ts
	}
	if isPost || isVoid { // takes from its pending transfer what it left zero
		wantRec.DebitAccountID, wantRec.CreditAccountID = pend.DebitAccountID, pend.CreditAccountID
		wantRec.Ledger, wantRec.Code = pend.Ledger, pend.Code
		wantRec.UserData64 = cmp.Or(in.UserData64, pend.UserData64)
		wantRec.UserData32 = cmp.Or(in.UserData32, pend.UserData32)
	}
	if rec != wantRec {
		t.Errorf("stored record mismatch\nwant:%+v\ngot :%+v", wantRec, rec)
	}

	// Balances: debits on dr mirror credits on cr.
	wantDrBalance, wantCrBalance := test.dr.Balance, test.cr.Balance
	switch {
	case isPending:
		wantDrBalance.DebitsPending += wantAmount
		wantCrBalance.CreditsPending += wantAmount
	case isPost, isVoid:
		wantDrBalance.DebitsPending -= pend.Amount // the whole reservation is released
		wantCrBalance.CreditsPending -= pend.Amount
		if isPost {
			wantDrBalance.DebitsPosted += wantAmount
			wantCrBalance.CreditsPosted += wantAmount
		}
	default:
		wantDrBalance.DebitsPosted += wantAmount
		wantCrBalance.CreditsPosted += wantAmount
	}

	// Flags: only AccountClosed moves, by a closing transfer and by its void.
	var wantDrFlagsSet, wantCrFlagsSet, wantDrFlagsCleared, wantCrFlagsCleared AccountFlags
	if in.Flags.Has(TransferClosingDebit) {
		wantDrFlagsSet = AccountClosed
	}
	if in.Flags.Has(TransferClosingCredit) {
		wantCrFlagsSet = AccountClosed
	}
	if isVoid && pend.Flags.Has(TransferClosingDebit) {
		wantDrFlagsCleared = AccountClosed
	}
	if isVoid && pend.Flags.Has(TransferClosingCredit) {
		wantCrFlagsCleared = AccountClosed
	}

	// Whole accounts, so that nothing else changed either.
	wantDr, wantCr := test.dr, test.cr
	wantDr.Balance, wantCr.Balance = wantDrBalance, wantCrBalance
	wantDr.Flags = (test.dr.Flags | wantDrFlagsSet) &^ wantDrFlagsCleared
	wantCr.Flags = (test.cr.Flags | wantCrFlagsSet) &^ wantCrFlagsCleared
	if dr != wantDr {
		t.Errorf("debitor mismatch\nwant:%+v\ngot :%+v", wantDr, dr)
	}
	if cr != wantCr {
		t.Errorf("creditor mismatch\nwant:%+v\ngot :%+v", wantCr, cr)
	}
	if test.pending != nil && auxpend != *test.pending {
		t.Error("on success immutable pending promise broken")
	}

	// Invariants: true of every accepted transfer, whatever the wants above say.
	if dr.DebitsPending-test.dr.DebitsPending != cr.CreditsPending-test.cr.CreditsPending ||
		dr.DebitsPosted-test.dr.DebitsPosted != cr.CreditsPosted-test.cr.CreditsPosted {
		t.Error("invariant: debits moved on dr differ from credits moved on cr")
	}
	if dr.DebitsPosted < test.dr.DebitsPosted || cr.CreditsPosted < test.cr.CreditsPosted {
		t.Error("invariant: a posted counter went down")
	}
	grew := dr.DebitsPending > test.dr.DebitsPending || cr.CreditsPending > test.cr.CreditsPending
	shrank := dr.DebitsPending < test.dr.DebitsPending || cr.CreditsPending < test.cr.CreditsPending
	if grew && !isPending || shrank && !(isPost || isVoid) {
		t.Error("invariant: a pending counter moved the wrong way")
	}
	for _, a := range []Account{dr, cr} {
		debits, credits := sum64(a.DebitsPending, a.DebitsPosted), sum64(a.CreditsPending, a.CreditsPosted)
		if debits.hi != 0 || credits.hi != 0 {
			t.Errorf("invariant: account %d pending plus posted passes 2^64", a.ID)
		}
		if a.Flags.Has(AccountDebitsMustNotExceedCredits) && (u128{lo: a.CreditsPosted}).less(debits) {
			t.Errorf("invariant: account %d debits exceed credits", a.ID)
		}
		if a.Flags.Has(AccountCreditsMustNotExceedDebits) && (u128{lo: a.DebitsPosted}).less(credits) {
			t.Errorf("invariant: account %d credits exceed debits", a.ID)
		}
	}
	if !isVoid && (test.dr.Flags | test.cr.Flags).Has(AccountClosed) {
		t.Error("invariant: a closed account took a movement other than a void")
	}
	if rec.Timestamp <= dr.Timestamp || rec.Timestamp <= cr.Timestamp {
		t.Error("invariant: record does not postdate its accounts")
	}

	// Replay: the stored record rebuilds both accounts from their initial state.
	for _, a := range []struct{ before, after Account }{{test.dr, dr}, {test.cr, cr}} {
		got := a.before
		if code := ExecuteReplay(&got, &rec, pend); code != 0 {
			t.Errorf("replay on account %d: %s", got.ID, code)
		} else if got != a.after {
			t.Errorf("replay on account %d mismatch\nwant:%+v\ngot :%+v", got.ID, a.after, got)
		}
	}
}

// TestExecuteReplayErrors replays records onto an account whose history cannot have
// led to them: each is rejected and leaves the account unchanged.
func TestExecuteReplayErrors(t *testing.T) {
	b := newBuilder(0)
	p := b.Pending(idDebit, idCred, 50)
	post := b.Post(&p, 30) // as stored: amount resolved
	closing := b.Pending(idDebit, idCred, 0)
	closing.Flags |= TransferClosingDebit
	voidClosing := b.Void(&closing)
	plain := b.Transfer(idDebit, idCred, 1)
	cases := []struct {
		name string
		acc  Account
		t, p *Transfer
		want ErrCode
	}{
		{"post of a reservation never replayed", Account{ID: idDebit}, &post, &p, ErrPendingTransferNotFound},
		{"void of a closing transfer never replayed", Account{ID: idDebit}, &voidClosing, &closing, ErrPendingTransferNotFound},
		{"transfer on a closed account", Account{ID: idDebit, Flags: AccountClosed}, &plain, nil, ErrDebitAccountAlreadyClosed},
		{"transfer into a closed account", Account{ID: idCred, Flags: AccountClosed}, &plain, nil, ErrCreditAccountAlreadyClosed},
		{"closing a closed account", Account{ID: idDebit, Flags: AccountClosed}, &closing, nil, ErrDebitAccountAlreadyClosed},
		{"posted debits overflow", Account{ID: idDebit, Balance: Balance{DebitsPosted: math.MaxUint64}}, &plain, nil, ErrOverflowsDebitsPosted},
		{"pending credits overflow", Account{ID: idCred, Balance: Balance{CreditsPending: math.MaxUint64}}, &p, nil, ErrOverflowsCreditsPending},
		{"post overflows credits posted", Account{ID: idCred, Balance: Balance{CreditsPending: 50, CreditsPosted: math.MaxUint64}}, &post, &p, ErrOverflowsCreditsPosted},
	}
	for _, c := range cases {
		acc := c.acc
		if got := ExecuteReplay(&acc, c.t, c.p); got != c.want {
			t.Errorf("%s: ExecuteReplay = %v, want %v", c.name, got, c.want)
		}
		if acc != c.acc {
			t.Errorf("%s: failure changed account to %+v", c.name, acc)
		}
	}
}

func TestExecutePanicsOnCallerBugs(t *testing.T) {
	b := newBuilder(0)
	plain := b.Transfer(idDebit, idCred, 5)
	p := b.Pending(idDebit, idCred, 50)
	post := b.Post(&p, AmountMax)
	other := b.Pending(idDebit, idCred, 50)
	cases := []struct {
		name string
		run  func(dr, cr *Account)
	}{
		{"pending transfer for a plain t", func(dr, cr *Account) { tr := plain; Execute(dr, cr, &tr, &p, PendingActive, execTS) }},
		{"wrong pending transfer", func(dr, cr *Account) { tr := post; Execute(dr, cr, &tr, &other, PendingActive, execTS) }},
		{"wrong debit account", func(dr, cr *Account) { tr := plain; Execute(cr, cr, &tr, nil, PendingNone, execTS) }},
		{"resolve without accounts", func(dr, cr *Account) { tr := post; Execute(nil, cr, &tr, &p, PendingActive, execTS) }},
		{"pending with status none", func(dr, cr *Account) { tr := post; Execute(dr, cr, &tr, &p, PendingNone, execTS) }},
		{"reservation missing from counters", func(dr, cr *Account) { tr := post; Execute(dr, cr, &tr, &p, PendingActive, execTS) }},
		{"non-positive timestamp", func(dr, cr *Account) { tr := plain; Execute(dr, cr, &tr, nil, PendingNone, 0) }},
		{"replay onto an unrelated account", func(dr, cr *Account) { stranger := Account{ID: 99}; ExecuteReplay(&stranger, &plain, nil) }},
		{"replay with wrong pending", func(dr, cr *Account) { ExecuteReplay(dr, &post, &other) }},
		{"replay of a post without its pending", func(dr, cr *Account) { ExecuteReplay(dr, &post, nil) }},
		{"replay of a plain with a pending", func(dr, cr *Account) { ExecuteReplay(dr, &plain, &p) }},
	}
	for _, c := range cases {
		dr, cr := accounts() // no reservation applied: the resolve cases see inconsistent counters
		mustPanic(t, c.name, func() { c.run(&dr, &cr) })
	}
}

// TestExecuteRandomMatchesReplay runs random transfers through Execute over a small
// store. After every step the ledger balances and no account breaks its flag; at the
// end, replaying the stored records with ExecuteReplay rebuilds every account exactly.
func TestExecuteRandomMatchesReplay(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	b := newBuilder(0)
	accts := []Account{
		{ID: 1, Ledger: 7, Code: 1, Timestamp: 1},
		{ID: 2, Ledger: 7, Code: 1, Timestamp: 2, Flags: AccountDebitsMustNotExceedCredits},
		{ID: 3, Ledger: 7, Code: 1, Timestamp: 3, Flags: AccountCreditsMustNotExceedDebits},
		{ID: 4, Ledger: 7, Code: 1, Timestamp: 4},
	}
	initial := slices.Clone(accts) // as created, for the replay
	byID := func(in []Account, id ID) *Account {
		for i := range in {
			if in[i].ID == id {
				return &in[i]
			}
		}
		return nil
	}
	var log []Transfer    // accepted records, in order
	index := map[ID]int{} // record ID to its position in log
	status := map[ID]PendingStatus{}
	var pendingIDs []ID // every pending transfer ever accepted, any status
	codes := map[ErrCode]int{}

	for step := range 6000 {
		ts := int64(100 + step)
		d := rng.IntN(len(accts))
		c := (d + 1 + rng.IntN(len(accts)-1)) % len(accts)
		var tr Transfer
		var p *Transfer
		ps := PendingNone
		switch op := rng.IntN(10); {
		case op < 3 || op >= 7 && len(pendingIDs) == 0: // nothing to resolve yet
			tr = b.Transfer(accts[d].ID, accts[c].ID, rng.Uint64N(1000))
		case op < 6:
			tr = b.Pending(accts[d].ID, accts[c].ID, rng.Uint64N(1000))
			if rng.IntN(300) == 0 { // rare: a closed account stays shut until the close is voided
				tr.Flags |= TransferClosingCredit
			}
		case op == 6:
			tr = b.Transfer(accts[d].ID, accts[c].ID, AmountMax)
			tr.Flags = TransferBalancingDebit
		default:
			pid := pendingIDs[rng.IntN(len(pendingIDs))]
			p = &log[index[pid]]
			ps = status[pid]
			if op < 9 {
				amount := rng.Uint64N(p.Amount + 2) // sometimes one above the reservation
				if rng.IntN(4) == 0 {
					amount = AmountMax
				}
				tr = Transfer{ID: b.ID(), PendingID: pid, Amount: amount, Flags: TransferPostPending}
			} else {
				tr = Transfer{ID: b.ID(), PendingID: pid, Flags: TransferVoidPending}
			}
		}
		var dr, cr *Account
		if p != nil {
			dr, cr = byID(accts, p.DebitAccountID), byID(accts, p.CreditAccountID)
		} else {
			dr, cr = byID(accts, tr.DebitAccountID), byID(accts, tr.CreditAccountID)
		}
		before := append([]Account(nil), accts...)
		code := Execute(dr, cr, &tr, p, ps, ts)
		codes[code]++
		if code != 0 {
			for i := range accts {
				if accts[i] != before[i] {
					t.Fatalf("step %d: rejected %v changed account %d", step, code, accts[i].ID)
				}
			}
			continue
		}
		index[tr.ID] = len(log)
		log = append(log, tr)
		switch {
		case tr.IsPending():
			status[tr.ID] = PendingActive
			pendingIDs = append(pendingIDs, tr.ID)
		case tr.Flags.Has(TransferPostPending):
			status[tr.PendingID] = PendingPosted
		case tr.Flags.Has(TransferVoidPending):
			status[tr.PendingID] = PendingVoided
		}

		if !balanced(accts) {
			t.Fatalf("step %d: debits and credits differ", step)
		}
		a2, a3 := &accts[1], &accts[2]
		if a2.DebitsPending+a2.DebitsPosted > a2.CreditsPosted || a3.CreditsPending+a3.CreditsPosted > a3.DebitsPosted {
			t.Fatalf("step %d: a constraint flag was broken: %+v %+v", step, a2.Balance, a3.Balance)
		}
	}

	replayed := initial
	for _, rec := range log {
		var p *Transfer
		if rec.IsResolving() {
			p = &log[index[rec.PendingID]]
		}
		// A stored record names its accounts, even a post or void that inherited them.
		for _, id := range [2]ID{rec.DebitAccountID, rec.CreditAccountID} {
			if code := ExecuteReplay(byID(replayed, id), &rec, p); code != 0 {
				t.Fatalf("ExecuteReplay rejected accepted record %+v on account %d: %v", rec, id, code)
			}
		}
	}
	for i := range accts {
		if replayed[i] != accts[i] {
			t.Errorf("account %d: Execute left %+v, replay gives %+v", accts[i].ID, accts[i], replayed[i])
		}
	}
	t.Logf("codes: %v", codes)
	for _, want := range []ErrCode{0, ErrExceedsCredits, ErrExceedsDebits, ErrPendingTransferAlreadyPosted,
		ErrPendingTransferAlreadyVoided, ErrExceedsPendingTransferAmount} {
		if codes[want] == 0 {
			t.Errorf("random run never produced %v; widen the generator", want)
		}
	}
}

// mustPanic fails t unless f panics.
func mustPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: did not panic", name)
		}
	}()
	f()
}

// balanced reports whether debits equal credits over accts, pending and posted
// separately: the double-entry invariant when accts are every account of a ledger.
// Test amounts are small, so these sums cannot overflow.
func balanced(accts []Account) bool {
	var b Balance
	for i := range accts {
		b.DebitsPending += accts[i].DebitsPending
		b.DebitsPosted += accts[i].DebitsPosted
		b.CreditsPending += accts[i].CreditsPending
		b.CreditsPosted += accts[i].CreditsPosted
	}
	return b.DebitsPending == b.CreditsPending && b.DebitsPosted == b.CreditsPosted
}

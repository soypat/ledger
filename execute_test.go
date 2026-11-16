package ledger

import (
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

// mustExecute fails t unless Execute accepts tr.
func mustExecute(t *testing.T, tr *Transfer, dr, cr *Account, p *Transfer, status PendingStatus) {
	t.Helper()
	if code := Execute(tr, execTS, dr, cr, p, status); code != 0 {
		t.Fatalf("Execute(%+v) = %v", *tr, code)
	}
}

func TestExecute_table(t *testing.T) {
	const (
		ledger = 7
		idDeb  = 2
		idCred = 3
	)
	account := func(id ID, ts int64, b Balance) Account {
		return Account{
			ID:        id,
			Ledger:    ledger,
			Code:      1,
			Balance:   b,
			Timestamp: ts,
		}
	}
	B := func() Balance {
		return Balance{}
	}
	sub := func() Transfer {

	}

	var tests = []struct {
		name      string
		dr, cr    Account
		submitted Transfer
		pending   *Transfer
		status    PendingStatus // if status 0 then set to reasonable value based on what looks to be the operation being performed.
		ts        int64
		// Outcome.
		wantCode ErrCode // No panic test here.
		// Success.
		// wantDebitorBalance  Balance
		// wantCreditorBalance Balance
	}{
		{
			name: "normal",
			dr:   account(idDeb, 100, B()), cr: account(idDeb, 100, B()),
			submitted: sub(), pending: nil,
		},
	}
	var auxpend Transfer
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dr := test.dr
			cr := test.cr
			sub := test.submitted
			var pend *Transfer
			if test.pending != nil {
				auxpend = *test.pending
				pend = &auxpend
			}
			ts := test.ts
			if ts == 0 {
				ts = 1
			}
			code := Execute(&dr, &cr, &sub, pend, test.status, ts)
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
			// Behavioural testing of values.
			wantCrBalance := Balance{}
			if wantCrBalance != cr.Balance {
				t.Errorf("creditor balance mismatch\nwant:%+v\ngot :%+v", wantCrBalance, cr.Balance)
			}
			wantDrBalance := Balance{}
			if wantDrBalance != dr.Balance {
				t.Errorf("debitor balance mismatch\nwant:%+v\ngot :%+v", wantDrBalance, dr.Balance)
			}
			var wantCrFlagsSet, wantDrFlagsSet AccountFlags
			// ... behavioural tests.
		})
	}
}

func TestExecutePlainAndPending(t *testing.T) {
	dr, cr := accounts()
	b := newBuilder(0)

	plain := b.Transfer(idDebit, idCred, 100)
	mustExecute(t, &plain, &dr, &cr, nil, PendingNone)
	if dr.Balance != (Balance{DebitsPosted: 100}) || cr.Balance != (Balance{CreditsPosted: 100}) {
		t.Errorf("after plain: dr %+v cr %+v", dr.Balance, cr.Balance)
	}
	if plain.Timestamp != execTS || plain.Amount != 100 {
		t.Errorf("stored plain = %+v", plain)
	}

	p := b.Pending(idDebit, idCred, 50)
	mustExecute(t, &p, &dr, &cr, nil, PendingNone)
	if dr.Balance != (Balance{DebitsPending: 50, DebitsPosted: 100}) || cr.Balance != (Balance{CreditsPending: 50, CreditsPosted: 100}) {
		t.Errorf("after pending: dr %+v cr %+v", dr.Balance, cr.Balance)
	}

	zero := b.Transfer(idDebit, idCred, 0) // allowed since TigerBeetle 0.16
	mustExecute(t, &zero, &dr, &cr, nil, PendingNone)
	if dr.DebitsPosted != 100 {
		t.Errorf("zero transfer moved money: %+v", dr.Balance)
	}
}

func TestExecutePostAndVoid(t *testing.T) {
	b := newBuilder(0)
	cases := []struct {
		name       string
		resolve    func(p *Transfer) Transfer
		wantAmount uint64
		wantPosted uint64
	}{
		{"partial post", func(p *Transfer) Transfer { return b.Post(p, 30) }, 30, 30},
		{"full post", func(p *Transfer) Transfer { return b.Post(p, AmountMax) }, 50, 50},
		{"zero post", func(p *Transfer) Transfer { return b.Post(p, 0) }, 0, 0},
		{"void", func(p *Transfer) Transfer { return b.Void(p) }, 50, 0},
		{"void of zero", func(p *Transfer) Transfer {
			return Transfer{ID: b.ID(), PendingID: p.ID, Flags: TransferVoidPending}
		}, 50, 0},
	}
	for _, c := range cases {
		dr, cr := accounts()
		p := b.Pending(idDebit, idCred, 50)
		p.UserData64, p.UserData32 = 77, 88
		mustExecute(t, &p, &dr, &cr, nil, PendingNone)

		tr := c.resolve(&p)
		tr.DebitAccountID, tr.CreditAccountID, tr.Ledger, tr.Code = 0, 0, 0, 0 // inherit all
		tr.UserData32 = 99                                                     // own value kept
		mustExecute(t, &tr, &dr, &cr, &p, PendingActive)
		want := Transfer{ID: tr.ID, DebitAccountID: idDebit, CreditAccountID: idCred, PendingID: p.ID,
			Amount: c.wantAmount, UserData64: 77, UserData32: 99, Ledger: 7, Code: 3, Flags: tr.Flags, Timestamp: execTS}
		if tr != want {
			t.Errorf("%s: stored %+v, want %+v", c.name, tr, want)
		}
		if dr.Balance != (Balance{DebitsPosted: c.wantPosted}) || cr.Balance != (Balance{CreditsPosted: c.wantPosted}) {
			t.Errorf("%s: dr %+v cr %+v; reservation not fully released", c.name, dr.Balance, cr.Balance)
		}
	}
}

func TestExecuteBalancing(t *testing.T) {
	b := newBuilder(0)
	dr, cr := accounts()
	dr.Flags = AccountDebitsMustNotExceedCredits
	dr.CreditsPosted, dr.DebitsPosted = 100, 30 // 70 of room
	cr.DebitsPosted = 1000

	tr := b.Transfer(idDebit, idCred, AmountMax)
	tr.Flags = TransferBalancingDebit
	mustExecute(t, &tr, &dr, &cr, nil, PendingNone)
	if tr.Amount != 70 || dr.DebitsPosted != 100 {
		t.Errorf("balancing debit moved %d, dr %+v; want 70", tr.Amount, dr.Balance)
	}

	again := b.Transfer(idDebit, idCred, 5)
	again.Flags = TransferBalancingDebit
	mustExecute(t, &again, &dr, &cr, nil, PendingNone) // no room left: moves zero, no error
	if again.Amount != 0 || dr.DebitsPosted != 100 {
		t.Errorf("balancing with no room moved %d", again.Amount)
	}

	credit := b.Transfer(idDebit, idCred, AmountMax)
	credit.Flags = TransferBalancingCredit | TransferPending
	cr.Flags = AccountCreditsMustNotExceedDebits // credits 70 so far, debits posted 1000
	dr.CreditsPosted = math.MaxUint64 / 2        // unconstrained on this side for the test
	mustExecute(t, &credit, &dr, &cr, nil, PendingNone)
	if credit.Amount != 1000-70 || cr.CreditsPending != 930 {
		t.Errorf("balancing credit moved %d, cr %+v; want 930", credit.Amount, cr.Balance)
	}
}

func TestExecuteClosing(t *testing.T) {
	b := newBuilder(0)
	dr, cr := accounts()
	other := b.Pending(idDebit, idCred, 5)
	mustExecute(t, &other, &dr, &cr, nil, PendingNone)

	closing := b.Pending(idDebit, idCred, 0)
	closing.Flags |= TransferClosingDebit
	mustExecute(t, &closing, &dr, &cr, nil, PendingNone)
	if !dr.Flags.Has(AccountClosed) || cr.Flags.Has(AccountClosed) {
		t.Fatalf("closing debit: dr %v cr %v", dr.Flags, cr.Flags)
	}

	plain := b.Transfer(idDebit, idCred, 1)
	if code := Execute(&dr, &cr, &plain, nil, PendingNone, execTS); code != ErrDebitAccountAlreadyClosed {
		t.Errorf("transfer on closed account = %v", code)
	}
	post := b.Post(&closing, AmountMax)
	if code := Execute(&post, execTS, &dr, &cr, &closing, PendingActive); code != ErrDebitAccountAlreadyClosed {
		t.Errorf("post of the closing transfer = %v; it can only be voided", code)
	}
	voidOther := b.Void(&other) // voids still flow through a closed account
	mustExecute(t, &voidOther, &dr, &cr, &other, PendingActive)
	if !dr.Flags.Has(AccountClosed) {
		t.Error("voiding an unrelated pending transfer reopened the account")
	}

	reopen := b.Void(&closing)
	mustExecute(t, &reopen, &dr, &cr, &closing, PendingActive)
	if dr.Flags.Has(AccountClosed) {
		t.Error("voiding the closing transfer did not reopen the account")
	}
	plain = b.Transfer(idDebit, idCred, 1)
	mustExecute(t, &plain, &dr, &cr, nil, PendingNone)
}

func TestExecuteClosingCredit(t *testing.T) {
	b := newBuilder(0)
	dr, cr := accounts()
	closing := b.Pending(idDebit, idCred, 0)
	closing.Flags |= TransferClosingCredit
	mustExecute(t, &closing, &dr, &cr, nil, PendingNone)
	if dr.Flags.Has(AccountClosed) || !cr.Flags.Has(AccountClosed) {
		t.Fatalf("closing credit: dr %v cr %v", dr.Flags, cr.Flags)
	}
	plain := b.Transfer(idDebit, idCred, 1)
	if code := Execute(&plain, execTS, &dr, &cr, nil, PendingNone); code != ErrCreditAccountAlreadyClosed {
		t.Errorf("transfer into closed account = %v", code)
	}
	reopen := b.Void(&closing)
	mustExecute(t, &reopen, &dr, &cr, &closing, PendingActive)
	if cr.Flags.Has(AccountClosed) {
		t.Error("voiding the closing transfer did not reopen the credit account")
	}
}

// TestExecuteVoidReopensOnlyItsSide voids a closing-debit transfer whose credit account
// another transfer closed: only the debit account reopens.
func TestExecuteVoidReopensOnlyItsSide(t *testing.T) {
	b := newBuilder(0)
	dr, cr := accounts()
	third := Account{ID: 30, Ledger: 7, Code: 1, Timestamp: 300}
	closeDr := b.Pending(idDebit, idCred, 0)
	closeDr.Flags |= TransferClosingDebit
	mustExecute(t, &closeDr, &dr, &cr, nil, PendingNone)
	closeCr := b.Pending(third.ID, idCred, 0)
	closeCr.Flags |= TransferClosingCredit
	mustExecute(t, &closeCr, &third, &cr, nil, PendingNone)

	void := b.Void(&closeDr)
	mustExecute(t, &void, &dr, &cr, &closeDr, PendingActive)
	if dr.Flags.Has(AccountClosed) || !cr.Flags.Has(AccountClosed) {
		t.Errorf("void of closing debit: dr %v cr %v; want only dr reopened", dr.Flags, cr.Flags)
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

// execCase is one Execute call over state built from the open accounts() pair.
type execCase struct {
	name   string
	t      Transfer
	dr, cr func(a *Account) // nil keeps the account as is
	noDr   bool             // pass dr as not found
	noCr   bool
	p      *Transfer
	status PendingStatus
	want   ErrCode
}

func (c *execCase) run(t *testing.T) {
	t.Helper()
	dr, cr := accounts()
	if c.dr != nil {
		c.dr(&dr)
	}
	if c.cr != nil {
		c.cr(&cr)
	}
	pdr, pcr := &dr, &cr
	if c.noDr {
		pdr = nil
	}
	if c.noCr {
		pcr = nil
	}
	tr := c.t
	drBefore, crBefore, trBefore := dr, cr, tr
	if got := Execute(&tr, execTS, pdr, pcr, c.p, c.status); got != c.want {
		t.Errorf("%s: Execute = %v, want %v", c.name, got, c.want)
	}
	if c.want != 0 && (dr != drBefore || cr != crBefore || tr != trBefore) {
		t.Errorf("%s: failure changed state: t %+v dr %+v cr %+v", c.name, tr, dr, cr)
	}
}

func TestExecuteErrors(t *testing.T) {
	const max = math.MaxUint64
	plain := Transfer{ID: 1, DebitAccountID: idDebit, CreditAccountID: idCred, Amount: 5, Ledger: 7, Code: 3}
	pend := plain
	pend.ID, pend.Amount, pend.Flags = 2, 50, TransferPending
	post := Transfer{ID: 3, PendingID: 2, Amount: AmountMax, Flags: TransferPostPending}
	void := Transfer{ID: 3, PendingID: 2, Flags: TransferVoidPending}
	notPending := plain
	notPending.ID = 2
	reserved := func(a *Account) { // the pending transfer's reservation, already applied
		if a.ID == idDebit {
			a.DebitsPending = 50
		} else {
			a.CreditsPending = 50
		}
	}
	with := func(base Transfer, mod func(t *Transfer)) Transfer { mod(&base); return base }
	closed := func(a *Account) { a.Flags |= AccountClosed }
	resolve := func(name string, tr Transfer, status PendingStatus, want ErrCode) execCase {
		return execCase{name: name, t: tr, dr: reserved, cr: reserved, p: &pend, status: status, want: want}
	}

	cases := []execCase{
		{name: "invalid transfer", t: with(plain, func(t *Transfer) { t.Ledger = 0 }), want: ErrLedgerMustNotBeZero},
		{name: "debit not found", t: plain, noDr: true, want: ErrDebitAccountNotFound},
		{name: "credit not found", t: plain, noCr: true, want: ErrCreditAccountNotFound},
		{name: "accounts on different ledgers", t: plain, cr: func(a *Account) { a.Ledger = 8 }, want: ErrAccountsMustHaveTheSameLedger},
		{name: "transfer on another ledger", t: with(plain, func(t *Transfer) { t.Ledger = 8 }), want: ErrTransferMustHaveTheSameLedgerAsAccounts},
		{name: "imported before debit account", t: with(plain, func(t *Transfer) { t.Flags, t.Timestamp = TransferImported, 100 }),
			want: ErrImportedEventTimestampMustPostdateDebitAccount},
		{name: "imported before credit account", t: with(plain, func(t *Transfer) { t.Flags, t.Timestamp = TransferImported, 150 }),
			want: ErrImportedEventTimestampMustPostdateCreditAccount},
		{name: "debit closed", t: plain, dr: closed, want: ErrDebitAccountAlreadyClosed},
		{name: "credit closed", t: plain, cr: closed, want: ErrCreditAccountAlreadyClosed},
		{name: "overflows debits pending", t: pend, dr: func(a *Account) { a.DebitsPending = max - 10 }, want: ErrOverflowsDebitsPending},
		{name: "overflows credits pending", t: pend, cr: func(a *Account) { a.CreditsPending = max - 10 }, want: ErrOverflowsCreditsPending},
		{name: "overflows debits posted", t: plain, dr: func(a *Account) { a.DebitsPosted = max - 1 }, want: ErrOverflowsDebitsPosted},
		{name: "overflows credits posted", t: plain, cr: func(a *Account) { a.CreditsPosted = max - 1 }, want: ErrOverflowsCreditsPosted},
		{name: "pending checks posted overflow too", t: pend, dr: func(a *Account) { a.DebitsPosted = max - 1 }, want: ErrOverflowsDebitsPosted},
		{name: "overflows debits", t: plain, dr: func(a *Account) { a.DebitsPending, a.DebitsPosted = max/2+1, max/2 }, want: ErrOverflowsDebits},
		{name: "overflows credits", t: plain, cr: func(a *Account) { a.CreditsPending, a.CreditsPosted = max/2+1, max/2 }, want: ErrOverflowsCredits},
		{name: "exceeds credits", t: plain, dr: func(a *Account) { a.Flags, a.CreditsPosted = AccountDebitsMustNotExceedCredits, 4 }, want: ErrExceedsCredits},
		{name: "exceeds credits counts pending", t: plain, dr: func(a *Account) {
			a.Flags, a.CreditsPosted, a.DebitsPending = AccountDebitsMustNotExceedCredits, 10, 6
		}, want: ErrExceedsCredits},
		{name: "exceeds debits", t: plain, cr: func(a *Account) { a.Flags, a.DebitsPosted = AccountCreditsMustNotExceedDebits, 4 }, want: ErrExceedsDebits},

		{name: "pending transfer not found", t: post, want: ErrPendingTransferNotFound},
		{name: "pending transfer not pending", t: post, p: &notPending, want: ErrPendingTransferNotPending},
		resolve("different debit account", with(post, func(t *Transfer) { t.DebitAccountID = 99 }), PendingActive, ErrPendingTransferHasDifferentDebitAccountID),
		resolve("different credit account", with(post, func(t *Transfer) { t.CreditAccountID = 99 }), PendingActive, ErrPendingTransferHasDifferentCreditAccountID),
		resolve("equal accounts differ from pending", with(post, func(t *Transfer) { t.DebitAccountID, t.CreditAccountID = idDebit, idDebit }),
			PendingActive, ErrPendingTransferHasDifferentCreditAccountID),
		resolve("different ledger", with(post, func(t *Transfer) { t.Ledger = 8 }), PendingActive, ErrPendingTransferHasDifferentLedger),
		resolve("different code", with(post, func(t *Transfer) { t.Code = 9 }), PendingActive, ErrPendingTransferHasDifferentCode),
		resolve("post above the reservation", with(post, func(t *Transfer) { t.Amount = 51 }), PendingActive, ErrExceedsPendingTransferAmount),
		resolve("void above the reservation", with(void, func(t *Transfer) { t.Amount = 51 }), PendingActive, ErrExceedsPendingTransferAmount),
		resolve("void below the reservation", with(void, func(t *Transfer) { t.Amount = 49 }), PendingActive, ErrPendingTransferHasDifferentAmount),
		resolve("already posted", post, PendingPosted, ErrPendingTransferAlreadyPosted),
		resolve("already voided", void, PendingVoided, ErrPendingTransferAlreadyVoided),
		resolve("imported post before debit account", with(post, func(t *Transfer) { t.Flags, t.Timestamp = t.Flags|TransferImported, 100 }),
			PendingActive, ErrImportedEventTimestampMustPostdateDebitAccount),
		resolve("imported post before credit account", with(post, func(t *Transfer) { t.Flags, t.Timestamp = t.Flags|TransferImported, 150 }),
			PendingActive, ErrImportedEventTimestampMustPostdateCreditAccount),
		{name: "post on closed debit", t: post, p: &pend, status: PendingActive,
			dr: func(a *Account) { reserved(a); closed(a) }, cr: reserved, want: ErrDebitAccountAlreadyClosed},
		{name: "post on closed credit", t: post, p: &pend, status: PendingActive,
			dr: reserved, cr: func(a *Account) { reserved(a); closed(a) }, want: ErrCreditAccountAlreadyClosed},
		{name: "void on closed account", t: void, p: &pend, status: PendingActive,
			dr: func(a *Account) { reserved(a); closed(a) }, cr: reserved, want: 0},
	}
	for i := range cases {
		cases[i].run(t)
	}
}

// TestExecutePrecedence breaks two rules at once; TigerBeetle's order picks the code.
func TestExecutePrecedence(t *testing.T) {
	const max = math.MaxUint64
	plain := Transfer{ID: 1, DebitAccountID: idDebit, CreditAccountID: idCred, Amount: 5, Ledger: 7, Code: 3}
	pend := plain
	pend.ID, pend.Amount, pend.Flags = 2, 50, TransferPending
	post := Transfer{ID: 3, PendingID: 2, Amount: 51, Flags: TransferPostPending} // above the reservation
	reserved := func(a *Account) { a.DebitsPending, a.CreditsPending = 50, 50 }
	cases := []execCase{
		{name: "validation before lookup", t: Transfer{ID: 1, DebitAccountID: idDebit, CreditAccountID: idCred, Code: 3},
			noDr: true, want: ErrLedgerMustNotBeZero},
		{name: "not found before ledgers", t: plain, noDr: true, cr: func(a *Account) { a.Ledger = 8 }, want: ErrDebitAccountNotFound},
		{name: "ledgers before closed", t: plain, cr: func(a *Account) { a.Ledger = 8 }, dr: func(a *Account) { a.Flags = AccountClosed },
			want: ErrAccountsMustHaveTheSameLedger},
		{name: "closed before overflow", t: plain, dr: func(a *Account) { a.Flags, a.DebitsPosted = AccountClosed, max },
			want: ErrDebitAccountAlreadyClosed},
		{name: "overflow before exceeds", t: plain, dr: func(a *Account) { a.Flags, a.DebitsPosted = AccountDebitsMustNotExceedCredits, max-1 },
			want: ErrOverflowsDebitsPosted},
		{name: "pending overflow before posted", t: pend, dr: func(a *Account) { a.DebitsPending, a.DebitsPosted = max-1, max-1 },
			want: ErrOverflowsDebitsPending},
		{name: "different account before amount", t: Transfer{ID: 3, PendingID: 2, DebitAccountID: 99, Amount: 51, Flags: TransferPostPending},
			p: &pend, status: PendingActive, dr: reserved, cr: reserved, want: ErrPendingTransferHasDifferentDebitAccountID},
		{name: "amount before status", t: post, p: &pend, status: PendingPosted, dr: reserved, cr: reserved, want: ErrExceedsPendingTransferAmount},
		{name: "status before closed", t: Transfer{ID: 3, PendingID: 2, Amount: AmountMax, Flags: TransferPostPending},
			p: &pend, status: PendingPosted, dr: func(a *Account) { reserved(a); a.Flags = AccountClosed }, cr: reserved,
			want: ErrPendingTransferAlreadyPosted},
	}
	for i := range cases {
		cases[i].run(t)
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
		{"pending transfer for a plain t", func(dr, cr *Account) { tr := plain; Execute(&tr, execTS, dr, cr, &p, PendingActive) }},
		{"wrong pending transfer", func(dr, cr *Account) { tr := post; Execute(&tr, execTS, dr, cr, &other, PendingActive) }},
		{"wrong debit account", func(dr, cr *Account) { tr := plain; Execute(&tr, execTS, cr, cr, nil, PendingNone) }},
		{"resolve without accounts", func(dr, cr *Account) { tr := post; Execute(&tr, execTS, nil, cr, &p, PendingActive) }},
		{"pending with status none", func(dr, cr *Account) { tr := post; Execute(&tr, execTS, dr, cr, &p, PendingNone) }},
		{"reservation missing from counters", func(dr, cr *Account) { tr := post; Execute(&tr, execTS, dr, cr, &p, PendingActive) }},
		{"non-positive timestamp", func(dr, cr *Account) { tr := plain; Execute(&tr, 0, dr, cr, nil, PendingNone) }},
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
		code := Execute(&tr, ts, dr, cr, p, ps)
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

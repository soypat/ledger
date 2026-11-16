package ledger

import (
	"math"
	"testing"
	"time"
)

func TestFlagsStringAppend(t *testing.T) {
	cases := []struct {
		got, want string
	}{
		{AccountFlags(0).String(), ""},
		{AccountLinked.String(), "linked"},
		{(AccountLinked | AccountClosed).String(), "linked|closed"},
		{(AccountDebitsMustNotExceedCredits | AccountImported).String(), "debits<=credits|imported"},
		{(TransferPending | TransferClosingCredit).String(), "pending|closing-credit"},
		{(FilterDebits | FilterReversed).String(), "debits|reversed"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

func TestFlagsHas(t *testing.T) {
	f := TransferLinked | TransferPostPending
	if !f.Has(TransferPostPending) || f.Has(TransferVoidPending) {
		t.Errorf("Has on %v wrong", f)
	}
}

func TestStringAppendDoesNotAllocate(t *testing.T) {
	buf := make([]byte, 0, 64)
	f := TransferPending | TransferBalancingDebit | TransferImported
	n := testing.AllocsPerRun(100, func() { buf = f.StringAppend(buf[:0]) })
	if n != 0 {
		t.Errorf("StringAppend allocates %v times", n)
	}
}

func TestEnumStrings(t *testing.T) {
	if s := CreditNormal.String(); s != "credit normal" {
		t.Errorf("CreditNormal = %q", s)
	}
	if s := PendingVoided.String(); s != "voided" {
		t.Errorf("PendingVoided = %q", s)
	}
	if s := ErrExceedsCredits.Error(); s != "exceeds credits" {
		t.Errorf("ErrExceedsCredits = %q", s)
	}
	if s := OpQueryTransfers.String(); s != "query transfers" {
		t.Errorf("OpQueryTransfers = %q", s)
	}
	if !OpCreateTransfers.IsWrite() || OpLookupAccounts.IsWrite() {
		t.Error("Op.IsWrite wrong")
	}
	var err error = ErrLengthMismatch
	if s := err.Error(); s != "length mismatch" {
		t.Errorf("ErrLengthMismatch = %q", s)
	}
}

func TestResultErrorAppend(t *testing.T) {
	cases := []struct {
		r    Result
		want string
	}{
		{Result{Timestamp: 1}, "ok"},
		{Result{Code: ErrExceedsCredits}, "exceeds credits"},
		{Result{Code: _errMax}, "invalid result code"},
	}
	buf := make([]byte, 0, 64)
	for _, c := range cases {
		if got := string(c.r.ErrorAppend(buf[:0])); got != c.want {
			t.Errorf("ErrorAppend(%+v) = %q, want %q", c.r, got, c.want)
		}
	}
	r := Result{Code: ErrLinkedEventFailed}
	if n := testing.AllocsPerRun(100, func() { buf = r.ErrorAppend(buf[:0]) }); n != 0 {
		t.Errorf("ErrorAppend allocates %v times", n)
	}
}

func TestIDGenMonotonic(t *testing.T) {
	var g IDGen
	now := time.UnixMilli(1_700_000_000_000)
	a := g.Next(now.UnixNano())
	b := g.Next(now.UnixNano())                       // same millisecond
	c := g.Next(now.Add(-time.Hour).UnixNano())       // clock stepped back
	d := g.Next(now.Add(time.Millisecond).UnixNano()) // clock moves on
	if !(a < b && b < c && c < d) {
		t.Fatalf("not monotonic: %d %d %d %d", a, b, c, d)
	}
	// Time truncates to a tick (2^20 ns), so it lands within one tick before now.
	if d := now.Sub(a.Time()); d < 0 || d >= 1<<idTickShift {
		t.Errorf("a.Time() = %v, want within one tick before %v", a.Time(), now)
	}
	if a == 0 {
		t.Error("IDGen yielded reserved ID 0")
	}
}

func TestNetBounds(t *testing.T) {
	const max64, minMag = 1<<63 - 1, 1 << 63
	cases := []struct {
		name            string
		s               Side
		debits, credits uint64
		want            int64
		ok              bool
	}{
		{"zero", DebitNormal, 5, 5, 0, true},
		{"max positive", DebitNormal, max64, 0, max64, true},
		{"one past max", DebitNormal, max64 + 1, 0, 0, false},
		{"min negative", DebitNormal, 0, minMag, -minMag, true},
		{"one past min", DebitNormal, 0, minMag + 1, 0, false},
		{"credit side max", CreditNormal, 0, max64, max64, true},
		{"credit side past min", CreditNormal, math.MaxUint64, 0, 0, false},
		{"huge but close", DebitNormal, math.MaxUint64, math.MaxUint64 - 7, 7, true},
	}
	for _, c := range cases {
		got, ok := c.s.Net(c.debits, c.credits)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: Net = %d, %v; want %d, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}
func TestNetPanicsOnInvalidSide(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Net with Side 0 did not panic")
		}
	}()
	Side(0).Net(1, 2)
}

func TestAccountValidate(t *testing.T) {
	valid := Account{ID: 1, Ledger: 1, Code: 1}
	cases := []struct {
		name string
		mod  func(a *Account)
		want ErrCode
	}{
		{"valid", func(a *Account) {}, 0},
		{"closed on create", func(a *Account) { a.Flags = AccountClosed }, 0},
		{"imported", func(a *Account) { a.Flags, a.Timestamp = AccountImported, 1 }, 0},

		{"timestamp not imported", func(a *Account) { a.Timestamp = 1 }, ErrTimestampMustBeZero},
		{"imported zero timestamp", func(a *Account) { a.Flags = AccountImported }, ErrImportedEventTimestampOutOfRange},
		{"imported negative timestamp", func(a *Account) { a.Flags, a.Timestamp = AccountImported, -1 }, ErrImportedEventTimestampOutOfRange},
		{"unknown flag", func(a *Account) { a.Flags = 1 << 15 }, ErrReservedFlag},
		{"id zero", func(a *Account) { a.ID = 0 }, ErrIDMustNotBeZero},
		{"id max", func(a *Account) { a.ID = math.MaxUint64 }, ErrIDMustNotBeIntMax},
		{"both constraints", func(a *Account) {
			a.Flags = AccountDebitsMustNotExceedCredits | AccountCreditsMustNotExceedDebits
		}, ErrFlagsAreMutuallyExclusive},
		{"debits pending", func(a *Account) { a.DebitsPending = 1 }, ErrDebitsPendingMustBeZero},
		{"debits posted", func(a *Account) { a.DebitsPosted = 1 }, ErrDebitsPostedMustBeZero},
		{"credits pending", func(a *Account) { a.CreditsPending = 1 }, ErrCreditsPendingMustBeZero},
		{"credits posted", func(a *Account) { a.CreditsPosted = 1 }, ErrCreditsPostedMustBeZero},
		{"ledger zero", func(a *Account) { a.Ledger = 0 }, ErrLedgerMustNotBeZero},
		{"code zero", func(a *Account) { a.Code = 0 }, ErrCodeMustNotBeZero},

		{"order: timestamp before flag", func(a *Account) { a.Timestamp, a.Flags = 1, 1<<15 }, ErrTimestampMustBeZero},
		{"order: flag before id", func(a *Account) { a.Flags, a.ID = 1<<15, 0 }, ErrReservedFlag},
		{"order: id before counters", func(a *Account) { a.ID, a.CreditsPosted = 0, 1 }, ErrIDMustNotBeZero},
		{"order: counters before ledger", func(a *Account) { a.DebitsPosted, a.Ledger = 1, 0 }, ErrDebitsPostedMustBeZero},
	}
	for _, c := range cases {
		a := valid
		c.mod(&a)
		if got := a.Validate(); got != c.want {
			t.Errorf("%s: Validate() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTransferValidate(t *testing.T) {
	valid := Transfer{ID: 1, DebitAccountID: 10, CreditAccountID: 20, Amount: 5, Ledger: 1, Code: 1}
	post := Transfer{ID: 2, PendingID: 1, Flags: TransferPostPending} // inherits the rest
	cases := []struct {
		name string
		base Transfer
		mod  func(t *Transfer)
		want ErrCode
	}{
		{"valid", valid, func(t *Transfer) {}, 0},
		{"zero amount", valid, func(t *Transfer) { t.Amount = 0 }, 0}, // allowed since TigerBeetle 0.16
		{"pending", valid, func(t *Transfer) { t.Flags = TransferPending }, 0},
		{"pending closing", valid, func(t *Transfer) { t.Flags = TransferPending | TransferClosingDebit }, 0},
		{"pending balancing", valid, func(t *Transfer) { t.Flags = TransferPending | TransferBalancingCredit }, 0},
		{"imported", valid, func(t *Transfer) { t.Flags, t.Timestamp = TransferImported, 1 }, 0},
		{"post inheriting", post, func(t *Transfer) {}, 0},
		{"void inheriting", post, func(t *Transfer) { t.Flags = TransferVoidPending }, 0},
		{"post explicit", post, func(t *Transfer) { t.DebitAccountID, t.CreditAccountID, t.Amount = 10, 20, 5 }, 0},
		{"post equal accounts", post, func(t *Transfer) { t.DebitAccountID, t.CreditAccountID = 10, 10 }, 0}, // Execute rejects

		{"timestamp not imported", valid, func(t *Transfer) { t.Timestamp = 1 }, ErrTimestampMustBeZero},
		{"imported zero timestamp", valid, func(t *Transfer) { t.Flags = TransferImported }, ErrImportedEventTimestampOutOfRange},
		{"unknown flag", valid, func(t *Transfer) { t.Flags = 1 << 15 }, ErrReservedFlag},
		{"id zero", valid, func(t *Transfer) { t.ID = 0 }, ErrIDMustNotBeZero},
		{"id max", valid, func(t *Transfer) { t.ID = math.MaxUint64 }, ErrIDMustNotBeIntMax},

		{"post and void", post, func(t *Transfer) { t.Flags |= TransferVoidPending }, ErrFlagsAreMutuallyExclusive},
		{"post and pending", post, func(t *Transfer) { t.Flags |= TransferPending }, ErrFlagsAreMutuallyExclusive},
		{"post balancing", post, func(t *Transfer) { t.Flags |= TransferBalancingDebit }, ErrFlagsAreMutuallyExclusive},
		{"void closing", post, func(t *Transfer) { t.Flags = TransferVoidPending | TransferClosingCredit }, ErrFlagsAreMutuallyExclusive},
		{"post pending id zero", post, func(t *Transfer) { t.PendingID = 0 }, ErrPendingIDMustNotBeZero},
		{"post pending id max", post, func(t *Transfer) { t.PendingID = math.MaxUint64 }, ErrPendingIDMustNotBeIntMax},
		{"post pending id self", post, func(t *Transfer) { t.PendingID = t.ID }, ErrPendingIDMustBeDifferent},

		{"debit zero", valid, func(t *Transfer) { t.DebitAccountID = 0 }, ErrDebitAccountIDMustNotBeZero},
		{"debit max", valid, func(t *Transfer) { t.DebitAccountID = math.MaxUint64 }, ErrDebitAccountIDMustNotBeIntMax},
		{"credit zero", valid, func(t *Transfer) { t.CreditAccountID = 0 }, ErrCreditAccountIDMustNotBeZero},
		{"credit max", valid, func(t *Transfer) { t.CreditAccountID = math.MaxUint64 }, ErrCreditAccountIDMustNotBeIntMax},
		{"same account", valid, func(t *Transfer) { t.CreditAccountID = t.DebitAccountID }, ErrAccountsMustBeDifferent},
		{"plain with pending id", valid, func(t *Transfer) { t.PendingID = 9 }, ErrPendingIDMustBeZero},
		{"closing without pending", valid, func(t *Transfer) { t.Flags = TransferClosingCredit }, ErrClosingTransferMustBePending},
		{"ledger zero", valid, func(t *Transfer) { t.Ledger = 0 }, ErrLedgerMustNotBeZero},
		{"code zero", valid, func(t *Transfer) { t.Code = 0 }, ErrCodeMustNotBeZero},

		{"order: timestamp before flag", valid, func(t *Transfer) { t.Timestamp, t.Flags = 1, 1<<15 }, ErrTimestampMustBeZero},
		{"order: flag before id", valid, func(t *Transfer) { t.Flags, t.ID = 1<<15, 0 }, ErrReservedFlag},
		{"order: accounts before pending id", valid, func(t *Transfer) { t.DebitAccountID, t.PendingID = 0, 9 }, ErrDebitAccountIDMustNotBeZero},
		{"order: pending id before closing", valid, func(t *Transfer) { t.PendingID, t.Flags = 9, TransferClosingDebit }, ErrPendingIDMustBeZero},
		{"order: closing before ledger", valid, func(t *Transfer) { t.Flags, t.Ledger = TransferClosingDebit, 0 }, ErrClosingTransferMustBePending},
		{"order: exclusive flags before pending id", post, func(t *Transfer) { t.Flags |= TransferPending; t.PendingID = 0 }, ErrFlagsAreMutuallyExclusive},
	}
	for _, c := range cases {
		tr := c.base
		c.mod(&tr)
		if got := tr.Validate(); got != c.want {
			t.Errorf("%s: Validate() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestBuilderOutputValidates(t *testing.T) {
	b := newBuilder(3)
	tr := b.Transfer(10, 20, 500)
	p := b.Pending(10, 20, 500)
	for _, x := range []Transfer{tr, p, b.Post(&p, AmountMax), b.Post(&p, 200), b.Post(&p, 0), b.Void(&p), b.Reverse(&tr)} {
		if code := x.Validate(); code != 0 {
			t.Errorf("builder output %+v: Validate() = %v", x, code)
		}
	}
}

func TestPhasePredicates(t *testing.T) {
	cases := []struct {
		flags              TransferFlags
		pending, resolving bool
	}{
		{0, false, false},
		{TransferPending, true, false},
		{TransferPostPending, false, true},
		{TransferVoidPending | TransferLinked, false, true},
	}
	for _, c := range cases {
		tr := Transfer{Flags: c.flags}
		if tr.IsPending() != c.pending || tr.IsResolving() != c.resolving {
			t.Errorf("flags %v: IsPending %v IsResolving %v", c.flags, tr.IsPending(), tr.IsResolving())
		}
	}
	if !ID(0).IsReserved() || !ID(math.MaxUint64).IsReserved() || ID(1).IsReserved() {
		t.Error("IsReserved wrong")
	}
}

func TestFlagMasksCoverEveryBit(t *testing.T) {
	if accountFlagsKnown != AccountLinked|AccountDebitsMustNotExceedCredits|
		AccountCreditsMustNotExceedDebits|AccountClosed|AccountImported {
		t.Errorf("accountFlagsKnown = %v", accountFlagsKnown)
	}
	if transferFlagsKnown != TransferLinked|TransferPending|TransferPostPending|TransferVoidPending|
		TransferBalancingDebit|TransferBalancingCredit|TransferClosingDebit|TransferClosingCredit|TransferImported {
		t.Errorf("transferFlagsKnown = %v", transferFlagsKnown)
	}
}

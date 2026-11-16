package ledger

import (
	"math"
	"testing"
)

func TestBalanceSides(t *testing.T) {
	b := Balance{DebitsPending: 30, DebitsPosted: 100, CreditsPending: 5, CreditsPosted: 40}
	type pair struct {
		v  int64
		ok bool
	}
	p := func(v int64, ok bool) pair { return pair{v, ok} }
	cases := []struct {
		name      string
		got, want pair
	}{
		{"debit posted", p(b.Posted(DebitNormal)), pair{60, true}},
		{"credit posted", p(b.Posted(CreditNormal)), pair{-60, true}},
		{"debit pending", p(b.Pending(DebitNormal)), pair{25, true}},
		{"credit pending", p(b.Pending(CreditNormal)), pair{-25, true}},
		{"debit projected", p(b.Projected(DebitNormal)), pair{85, true}},
		{"credit projected", p(b.Projected(CreditNormal)), pair{-85, true}},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %+v, want %+v", c.name, c.got, c.want)
		}
	}
}

func TestProjectedExactPastPosted(t *testing.T) {
	// Posted alone overflows int64, but pending credits bring the projection back in range.
	b := Balance{DebitsPosted: math.MaxUint64, CreditsPending: math.MaxUint64 - 100}
	if _, ok := b.Posted(DebitNormal); ok {
		t.Error("Posted should not fit")
	}
	if v, ok := b.Projected(DebitNormal); !ok || v != 100 {
		t.Errorf("Projected = %d, %v; want 100, true", v, ok)
	}
	// Both sides carry past 2^64, so only a 128-bit sum keeps the result exact.
	b = Balance{DebitsPosted: math.MaxUint64, DebitsPending: 10, CreditsPosted: math.MaxUint64, CreditsPending: 3}
	if v, ok := b.Projected(DebitNormal); !ok || v != 7 {
		t.Errorf("Projected with carries = %d, %v; want 7, true", v, ok)
	}
}

func TestHeadroom(t *testing.T) {
	cases := []struct {
		name     string
		b        Balance
		debit    int64
		debitOK  bool
		credit   int64
		creditOK bool
	}{
		{"fresh", Balance{}, 0, true, 0, true},
		{"room left", Balance{CreditsPosted: 100, DebitsPending: 30, DebitsPosted: 50}, 20, true, -50, true},
		{"exactly full", Balance{CreditsPosted: 80, DebitsPending: 30, DebitsPosted: 50}, 0, true, -30, true},
		{"debits over", Balance{CreditsPosted: 10, DebitsPosted: 50}, -40, true, 40, true},
		{"credit side", Balance{DebitsPosted: 100, CreditsPending: 25}, -100, true, 75, true},
		{"debits sum past 2^64", Balance{CreditsPosted: 100, DebitsPending: math.MaxUint64, DebitsPosted: 2}, 0, false, -98, true},
	}
	for _, c := range cases {
		if got, ok := c.b.DebitHeadroom(); got != c.debit || ok != c.debitOK {
			t.Errorf("%s: DebitHeadroom = %d, %v; want %d, %v", c.name, got, ok, c.debit, c.debitOK)
		}
		if got, ok := c.b.CreditHeadroom(); got != c.credit || ok != c.creditOK {
			t.Errorf("%s: CreditHeadroom = %d, %v; want %d, %v", c.name, got, ok, c.credit, c.creditOK)
		}
	}
}

/*

func TestEffectLifecycle(t *testing.T) {
	const A, B = 10, 20
	b := newBuilder(0)
	var ba, bb Balance
	apply := func(tr, p *Transfer) {
		t.Helper()
		if !ba.Effect(A, tr, p) || !bb.Effect(B, tr, p) {
			t.Fatalf("Effect rejected accepted transfer %+v", tr)
		}
	}
	check := func(step string, wantA Balance) {
		t.Helper()
		// A and B are the only accounts, so B mirrors A and the pair balances.
		wantB := Balance{
			CreditsPending: wantA.DebitsPending, CreditsPosted: wantA.DebitsPosted,
			DebitsPending: wantA.CreditsPending, DebitsPosted: wantA.CreditsPosted,
		}
		if ba != wantA || bb != wantB {
			t.Errorf("%s: A=%+v B=%+v, want A=%+v B=%+v", step, ba, bb, wantA, wantB)
		}
		if !balanced([]Account{{Balance: ba}, {Balance: bb}}) {
			t.Errorf("%s: debits and credits differ", step)
		}
	}

	plain := b.Transfer(A, B, 100)
	apply(&plain, nil)
	check("plain", Balance{DebitsPosted: 100})

	p1 := b.Pending(A, B, 50)
	apply(&p1, nil)
	check("pending", Balance{DebitsPending: 50, DebitsPosted: 100})

	post := b.Post(&p1, 30)
	apply(&post, &p1)
	check("partial post", Balance{DebitsPosted: 130})

	p2 := b.Pending(A, B, 40)
	apply(&p2, nil)
	void := b.Void(&p2)
	apply(&void, &p2)
	check("void", Balance{DebitsPosted: 130})

	p3 := b.Pending(A, B, 15)
	apply(&p3, nil)
	postAll := Transfer{ID: b.ID(), PendingID: p3.ID, Amount: AmountMax, Flags: TransferPostPending} // inherits all
	apply(&postAll, &p3)
	check("inherited full post", Balance{DebitsPosted: 145})

	rev := b.Reverse(&plain)
	apply(&rev, nil)
	check("reversal", Balance{DebitsPosted: 145, CreditsPosted: 100})
}

// TestEffectFailuresLeaveBalanceUnchanged drives every way Effect reports a transfer
// that cannot follow from the history replayed so far.
func TestEffectFailuresLeaveBalanceUnchanged(t *testing.T) {
	const A, B = 10, 20
	b := newBuilder(0)
	p := b.Pending(A, B, 50)
	post := b.Post(&p, AmountMax)
	void := b.Void(&p)
	overPost := Transfer{ID: b.ID(), PendingID: p.ID, Amount: p.Amount + 1, Flags: TransferPostPending}
	plain := b.Transfer(A, B, 1)
	pend := b.Pending(A, B, 1)

	cases := []struct {
		name string
		id   ID
		b    Balance
		t, p *Transfer
	}{
		{"post of a pending never replayed", A, Balance{}, &post, &p},
		{"void of a pending never replayed", B, Balance{}, &void, &p},
		{"post above its reservation", A, Balance{DebitsPending: 50}, &overPost, &p},
		{"posted debits overflow", A, Balance{DebitsPosted: math.MaxUint64}, &plain, nil},
		{"posted credits overflow", B, Balance{CreditsPosted: math.MaxUint64}, &plain, nil},
		{"pending debits overflow", A, Balance{DebitsPending: math.MaxUint64}, &pend, nil},
		{"post overflows posted", A, Balance{DebitsPending: 50, DebitsPosted: math.MaxUint64}, &post, &p},
	}
	for _, c := range cases {
		bal := c.b
		if bal.Effect(c.id, c.t, c.p) {
			t.Errorf("%s: Effect reported success", c.name)
		}
		if bal != c.b {
			t.Errorf("%s: balance changed to %+v on failure", c.name, bal)
		}
	}
}

func TestEffectIgnoresOtherAccounts(t *testing.T) {
	b := newBuilder(0)
	tr := b.Transfer(10, 20, 100)
	var bal Balance
	if !bal.Effect(99, &tr, nil) || bal != (Balance{}) {
		t.Errorf("unrelated transfer: ok=false or balance changed: %+v", bal)
	}
}

func TestEffectPanicsOnWrongPending(t *testing.T) {
	b := newBuilder(0)
	p := b.Pending(10, 20, 50)
	other := b.Pending(10, 20, 50)
	post := b.Post(&p, 0)
	plain := b.Transfer(10, 20, 5)
	cases := []struct {
		name string
		t, p *Transfer
	}{
		{"post without pending", &post, nil},
		{"post with wrong pending", &post, &other},
		{"plain with a pending", &plain, &p},
	}
	for _, c := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", c.name)
				}
			}()
			var bal Balance
			bal.Effect(10, c.t, c.p)
		}()
	}
}

// TestEffectRandomKeepsLedgerBalanced replays random valid transfers between a few
// accounts and checks the double-entry invariant after every one.
func TestEffectRandomKeepsLedgerBalanced(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	b := newBuilder(0)
	accts := []Account{{ID: 1, Ledger: 7}, {ID: 2, Ledger: 7}, {ID: 3, Ledger: 7}, {ID: 4, Ledger: 7}}
	var pendings []Transfer
	apply := func(step int, tr, p *Transfer) {
		for i := range accts {
			if !accts[i].Effect(accts[i].ID, tr, p) {
				t.Fatalf("step %d: Effect rejected %+v on account %d", step, tr, accts[i].ID)
			}
		}
	}
	for step := range 5000 {
		d := rng.IntN(len(accts))
		c := (d + 1 + rng.IntN(len(accts)-1)) % len(accts)
		amount := 1 + rng.Uint64N(1000)
		switch op := rng.IntN(4); {
		case op == 0 || len(pendings) == 0 && op >= 2:
			tr := b.Transfer(accts[d].ID, accts[c].ID, amount)
			apply(step, &tr, nil)
		case op == 1:
			tr := b.Pending(accts[d].ID, accts[c].ID, amount)
			apply(step, &tr, nil)
			pendings = append(pendings, tr)
		default:
			k := rng.IntN(len(pendings))
			p := pendings[k]
			pendings = append(pendings[:k], pendings[k+1:]...)
			var tr Transfer
			if op == 2 {
				amount := rng.Uint64N(p.Amount + 1) // 0 posts nothing
				if rng.IntN(4) == 0 {
					amount = AmountMax // posts it all
				}
				tr = b.Post(&p, amount)
			} else {
				tr = b.Void(&p)
			}
			apply(step, &tr, &p)
		}
		if !balanced(accts) {
			t.Fatalf("step %d: debits and credits differ", step)
		}
	}
	var live, reserved uint64
	for _, p := range pendings {
		live += p.Amount
	}
	for i := range accts {
		reserved += accts[i].DebitsPending
	}
	if reserved != live {
		t.Errorf("debits pending %d, want sum of live pendings %d", reserved, live)
	}
}

func TestBalanceAt(t *testing.T) {
	b1, b2, b3 := Balance{DebitsPosted: 1}, Balance{DebitsPosted: 2}, Balance{DebitsPosted: 3}
	history := []AccountBalance{{b1, 10}, {b2, 20}, {b3, 30}}
	cases := []struct {
		ts   int64
		want Balance
		ok   bool
	}{
		{5, Balance{}, false}, {10, b1, true}, {15, b1, true}, {20, b2, true},
		{29, b2, true}, {30, b3, true}, {1000, b3, true},
	}
	for _, c := range cases {
		if got, ok := BalanceAt(history, c.ts); got != c.want || ok != c.ok {
			t.Errorf("BalanceAt(%d) = %+v, %v; want %+v, %v", c.ts, got, ok, c.want, c.ok)
		}
	}
	if got, ok := BalanceAt(nil, 10); ok || got != (Balance{}) {
		t.Errorf("BalanceAt on empty history = %+v, %v; want zero, false", got, ok)
	}
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

*/

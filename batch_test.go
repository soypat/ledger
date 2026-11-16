package ledger

import (
	"slices"
	"testing"
)

func TestLinkTransfers(t *testing.T) {
	b := newBuilder(0)
	chain := []Transfer{
		b.Pending(10, 20, 100),
		b.Pending(10, 30, 21),
		b.Transfer(30, 40, 5),
	}
	chain[2].Flags |= TransferLinked // stale flag on the last record must be cleared
	Link(chain)
	for i, want := range []bool{true, true, false} {
		if got := chain[i].Flags.Has(TransferLinked); got != want {
			t.Errorf("transfer %d linked = %v, want %v", i, got, want)
		}
		if code := chain[i].Validate(); code != 0 {
			t.Errorf("transfer %d: Validate() = %v", i, code)
		}
	}
	if !chain[0].IsPending() || !chain[1].IsPending() {
		t.Error("Link dropped other flags")
	}
}

func TestLinkAccounts(t *testing.T) {
	accts := []Account{
		{ID: 1, Ledger: 1, Code: 1, Flags: AccountDebitsMustNotExceedCredits},
		{ID: 2, Ledger: 1, Code: 1},
	}
	Link(accts)
	if accts[0].Flags != AccountLinked|AccountDebitsMustNotExceedCredits || accts[1].Flags != 0 {
		t.Errorf("flags after Link: %v, %v", accts[0].Flags, accts[1].Flags)
	}
}

func TestLinkShortChains(t *testing.T) {
	Link([]Transfer(nil)) // must not panic
	one := []Transfer{{Flags: TransferLinked}}
	Link(one)
	if one[0].Flags.Has(TransferLinked) {
		t.Error("a chain of one must not stay linked")
	}
}

func TestChainEnd(t *testing.T) {
	const L = TransferLinked
	batch := []Transfer{
		{},         // lone transfer: chain [0,1)
		{Flags: L}, // chain [1,4)
		{Flags: L}, //
		{},         //
		{},         // lone transfer: chain [4,5)
		{Flags: L}, // open chain [5,7): last record carries Linked
		{Flags: L}, //
	}
	var ends []int
	for i := 0; i < len(batch); i = ChainEnd(batch, i) {
		ends = append(ends, ChainEnd(batch, i))
	}
	if want := []int{1, 4, 5, 7}; !slices.Equal(ends, want) {
		t.Errorf("chain ends = %v, want %v", ends, want)
	}
	if ChainEnd(batch, len(batch)) != len(batch) {
		t.Error("ChainEnd past the end should return len(batch)")
	}
}

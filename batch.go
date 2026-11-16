package ledger

// A batch is a plain slice of accounts or transfers, as in TigerBeetle. Records
// marked Linked chain to the next one; a ledger applies a chain whole or not at all.

// linkable is a record that can join a linked chain: *Account or *Transfer.
type linkable[E any] interface {
	*E
	isLinked() bool
	setLinked(on bool)
}

// Link makes chain one linked chain of accounts or transfers: Linked is set on every
// record but the last and cleared on the last, so the chain is never left open.
func Link[E any, P linkable[E]](chain []E) {
	for i := range chain {
		P(&chain[i]).setLinked(i < len(chain)-1)
	}
}

// ChainEnd returns the index just past the chain starting at batch[i]: one past its first
// record without Linked, or len(batch) for a chain left open by the batch's last record.
// A record without Linked is a chain of one. Iterate chains with i = ChainEnd(batch, i).
func ChainEnd[E any, P linkable[E]](batch []E, i int) int {
	for ; i < len(batch); i++ {
		if !P(&batch[i]).isLinked() {
			return i + 1
		}
	}
	return len(batch)
}

func (a *Account) isLinked() bool { return a.Flags.Has(AccountLinked) }

func (a *Account) setLinked(on bool) {
	a.Flags &^= AccountLinked
	if on {
		a.Flags |= AccountLinked
	}
}

func (t *Transfer) isLinked() bool { return t.Flags.Has(TransferLinked) }

func (t *Transfer) setLinked(on bool) {
	t.Flags &^= TransferLinked
	if on {
		t.Flags |= TransferLinked
	}
}

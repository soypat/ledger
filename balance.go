package ledger

// AccountBalance is an account's counters right after the transfer at Timestamp.
type AccountBalance struct {
	Balance
	Timestamp int64
}

// Balance holds an account's four cumulative counters, in minor units of its ledger.
type Balance struct {
	DebitsPending  uint64 // reserved by pending transfers
	DebitsPosted   uint64 // committed
	CreditsPending uint64
	CreditsPosted  uint64
}

// Posted returns the committed balance on side s; false if it does not fit an int64.
func (b Balance) Posted(s Side) (int64, bool) { return s.Net(b.DebitsPosted, b.CreditsPosted) }

// Pending returns the net amount reserved on side s; false if it does not fit an int64.
func (b Balance) Pending(s Side) (int64, bool) { return s.Net(b.DebitsPending, b.CreditsPending) }

// Projected returns Posted + Pending, the balance once every pending transfer posts;
// false if it does not fit an int64. Exact even where Posted or Pending alone would not fit.
func (b Balance) Projected(s Side) (int64, bool) {
	return s.net(sum64(b.DebitsPosted, b.DebitsPending), sum64(b.CreditsPosted, b.CreditsPending))
}

// DebitHeadroom is how much more the account may be debited before the limit of
// AccountDebitsMustNotExceedCredits: credits posted less debits pending and posted.
// Negative if over by that much, which only an account without the flag can be;
// false if it does not fit an int64.
func (b Balance) DebitHeadroom() (int64, bool) {
	return CreditNormal.net(sum64(b.DebitsPending, b.DebitsPosted), u128{lo: b.CreditsPosted})
}

// CreditHeadroom is how much more the account may be credited before the limit of
// AccountCreditsMustNotExceedDebits: debits posted less credits pending and posted.
// Negative if over by that much, which only an account without the flag can be;
// false if it does not fit an int64.
func (b Balance) CreditHeadroom() (int64, bool) {
	return DebitNormal.net(u128{lo: b.DebitsPosted}, sum64(b.CreditsPending, b.CreditsPosted))
}

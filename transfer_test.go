package ledger

import "time"

// stepClock is a Clock that advances by step on every reading.
type stepClock struct{ now, step int64 }

func (c *stepClock) Now() int64 {
	if c.now == 0 {
		c.now = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano() // default start date.
	}
	c.now += c.step
	return c.now
}

func newBuilder(writer uint16) *TransferBuilder {
	var b TransferBuilder
	var clock stepClock
	b.Configure(TransferBuilderConfig{
		Nanotime: clock.Now,
		Ledger:   7,
		Code:     3,
		Writer:   writer,
	})
	return &b
}

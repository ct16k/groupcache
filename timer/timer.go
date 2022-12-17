package timer

import (
	"sync/atomic"
	"time"
	_ "unsafe"
)

// Timer holds representation of current time.
type Timer interface {
	// Give current time (in nanoseconds)
	Now() int64
}

// Default timer reads Unix time always when requested
type Default struct{}

func (t Default) Now() int64 {
	return time.Now().UnixNano()
}

// NanoTime returns the current value of the runtime clock in nanoseconds
//
//go:noescape
//go:linkname NanoTime runtime.nanotime
func NanoTime() int64

// Fast timer is a faster, less precise timer than the default
type Fast struct{}

func (t Fast) Now() int64 {
	return NanoTime()
}

// FastEpoch timer is a faster, less precise timer than the default, with a predifined epoch
type FastEpoch int64

func (t FastEpoch) Now() int64 {
	return int64(t) + NanoTime()
}

// Cached timer stores Unix time periodically and returns the cached value
type Cached struct {
	nowFunc func() int64
	now     int64
	ticker  *time.Ticker
}

// Create cached timer and start runtime timer that updates time every given interval
func NewCachedTimer(nowFunc func() int64, granularity time.Duration) Timer {
	t := &Cached{
		nowFunc: nowFunc,
		now:     nowFunc(),
		ticker:  time.NewTicker(granularity),
	}

	go t.update()

	return t
}

func (t *Cached) Now() int64 {
	return atomic.LoadInt64(&t.now)
}

// Stop runtime timer and finish routine that updates time
func (t *Cached) Stop() {
	t.ticker.Stop()
	t.ticker = nil
}

// Periodically check and update time
func (t *Cached) update() {
	for range t.ticker.C {
		atomic.StoreInt64(&t.now, t.nowFunc())
	}
}

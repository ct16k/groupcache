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

type TimerFunc func() int64

func (f TimerFunc) Now() int64 {
	return f()
}

// NanoTime returns the current value of the runtime clock in nanoseconds
//
//go:noescape
//go:linkname NanoTime runtime.nanotime
func NanoTime() int64

var (
	// Default timer reads Unix time always when requested
	Default = TimerFunc(func() int64 { return time.Now().UnixNano() })
	// Fast timer is a faster, less precise timer than the default
	Fast = TimerFunc(NanoTime)
	// FastEpoch timer is a faster, less precise timer than the default, with a predefined epoch
	FastEpoch = func(epoch int64) TimerFunc {
		return TimerFunc(func() int64 {
			return epoch + NanoTime()
		})
	}
)

// Cached timer stores Unix time periodically and returns the cached value
type Cached struct {
	timerFunc func() int64
	now       int64
	ticker    *time.Ticker
}

// Create cached timer and start runtime timer that updates time every given interval
func NewCachedTimer(timerFunc func() int64, granularity time.Duration) Timer {
	t := &Cached{
		timerFunc: timerFunc,
		now:       timerFunc(),
		ticker:    time.NewTicker(granularity),
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
		atomic.StoreInt64(&t.now, t.timerFunc())
	}
}

package timer

import "time"

var _ Timer = NewCachedTimer(NanoTime, time.Second)

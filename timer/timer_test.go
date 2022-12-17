package timer

import "time"

var (
	_ Timer = Default{}
	_ Timer = Fast{}
	_ Timer = FastEpoch(time.Now().UnixNano())
	_ Timer = NewCachedTimer(NanoTime, time.Second)
)

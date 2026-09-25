package store

import (
	"time"

	_ "modernc.org/sqlite"
)

// secondsCeiling separates the two timestamp encodings the sessions table
// has held. Values below it are Unix seconds written before nanosecond
// precision (a seconds timestamp stays under it until the year 33658); at
// or above it are Unix nanoseconds (any real nanosecond timestamp since
// 1970 far exceeds it). This lets decodeTime read old rows without a data
// migration.
const secondsCeiling int64 = 1e12

// encodeTime stores a timestamp as Unix nanoseconds so sessions launched in
// the same second keep a distinct, ordered launch time. The zero time
// encodes as 0.
func encodeTime(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

// decodeTime reverses encodeTime, reading pre-precision rows as seconds.
func decodeTime(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	if v < secondsCeiling {
		return time.Unix(v, 0)
	}
	return time.Unix(0, v)
}

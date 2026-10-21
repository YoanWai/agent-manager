package value

import "time"

// RetryWait is how long a host that failed its last refreshes rests before
// the next try: a poll apart at first, doubling to half a minute, so an
// unreachable host is not dialled on every poll for as long as it is down.
func RetryWait(failures int) time.Duration {
	const longest = 30 * time.Second
	wait := 2 * time.Second
	for i := 1; i < failures && wait < longest; i++ {
		wait *= 2
	}
	return min(wait, longest)
}

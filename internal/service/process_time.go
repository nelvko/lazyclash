package service

import "time"

func processStartedAt(microseconds uint64, now time.Time, monotonic time.Duration) time.Time {
	// Validate before conversion to avoid wrapping corrupt or future values.
	if microseconds == 0 || monotonic < 0 || microseconds > uint64(monotonic/time.Microsecond) {
		return time.Time{}
	}
	elapsed := monotonic - time.Duration(microseconds)*time.Microsecond
	return now.Add(-elapsed)
}

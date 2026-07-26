package trackerintake

import "time"

const (
	// ContinuationRetryDelay is Symphony's short re-check after a clean worker
	// exit.
	ContinuationRetryDelay = time.Second
	failureRetryBase       = 10 * time.Second
)

// FailureRetryDelay returns the Symphony failure backoff for a 1-based attempt,
// capped without an overflowing shift for large attempt counts.
func FailureRetryDelay(attempt int64, max time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if max <= 0 {
		max = 5 * time.Minute
	}
	delay := failureRetryBase
	for current := int64(1); current < attempt && delay < max; current++ {
		if delay > max/2 {
			return max
		}
		delay *= 2
	}
	if delay > max {
		return max
	}
	return delay
}

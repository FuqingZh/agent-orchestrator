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
func FailureRetryDelay(attempt int64, maximum time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if maximum <= 0 {
		maximum = 5 * time.Minute
	}
	delay := failureRetryBase
	for current := int64(1); current < attempt && delay < maximum; current++ {
		if delay > maximum/2 {
			return maximum
		}
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}

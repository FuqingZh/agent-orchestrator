package trackerintake

import (
	"testing"
	"time"
)

func TestFailureRetryDelay(t *testing.T) {
	tests := []struct {
		attempt int64
		max     time.Duration
		want    time.Duration
	}{
		{attempt: 0, max: 5 * time.Minute, want: 10 * time.Second},
		{attempt: 1, max: 5 * time.Minute, want: 10 * time.Second},
		{attempt: 2, max: 5 * time.Minute, want: 20 * time.Second},
		{attempt: 3, max: 5 * time.Minute, want: 40 * time.Second},
		{attempt: 20, max: 5 * time.Minute, want: 5 * time.Minute},
		{attempt: 2, max: 15 * time.Second, want: 15 * time.Second},
	}
	for _, test := range tests {
		if got := FailureRetryDelay(test.attempt, test.max); got != test.want {
			t.Errorf("attempt %d max %s: got %s, want %s", test.attempt, test.max, got, test.want)
		}
	}
	if ContinuationRetryDelay != time.Second {
		t.Fatalf("continuation delay = %s, want 1s", ContinuationRetryDelay)
	}
}

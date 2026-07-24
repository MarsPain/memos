package ai

import (
	"sync"
	"time"
)

// Limits bounds chat generation centrally: per-user send rate, global
// in-flight concurrency, per-attempt wall clock, and answer size.
type Limits struct {
	// MaxConcurrentAttempts caps assistant attempts generating at once.
	MaxConcurrentAttempts int
	// AttemptTimeout is the wall-clock deadline of one assistant attempt.
	AttemptTimeout time.Duration
	// MaxAnswerRunes caps the accumulated answer size of one attempt.
	MaxAnswerRunes int
	// SendRateBurst is the per-user send token bucket capacity.
	SendRateBurst int
	// SendRateInterval is the per-user token refill interval: one send token
	// refills per interval up to the burst capacity.
	SendRateInterval time.Duration
}

// DefaultLimits returns the provisional centralized chat limits.
func DefaultLimits() Limits {
	return Limits{
		MaxConcurrentAttempts: 8,
		AttemptTimeout:        2 * time.Minute,
		MaxAnswerRunes:        64 * 1024,
		SendRateBurst:         6,
		SendRateInterval:      2 * time.Second,
	}
}

// normalize fills zero or negative fields with the defaults.
func (limits Limits) normalize() Limits {
	defaults := DefaultLimits()
	if limits.MaxConcurrentAttempts <= 0 {
		limits.MaxConcurrentAttempts = defaults.MaxConcurrentAttempts
	}
	if limits.AttemptTimeout <= 0 {
		limits.AttemptTimeout = defaults.AttemptTimeout
	}
	if limits.MaxAnswerRunes <= 0 {
		limits.MaxAnswerRunes = defaults.MaxAnswerRunes
	}
	if limits.SendRateBurst <= 0 {
		limits.SendRateBurst = defaults.SendRateBurst
	}
	if limits.SendRateInterval <= 0 {
		limits.SendRateInterval = defaults.SendRateInterval
	}
	return limits
}

// sendLimiter bounds new generations per user with a token bucket.
type sendLimiter struct {
	mu       sync.Mutex
	burst    float64
	interval time.Duration
	buckets  map[int32]*sendBucket
}

type sendBucket struct {
	tokens float64
	last   time.Time
}

func newSendLimiter(burst int, interval time.Duration) *sendLimiter {
	return &sendLimiter{
		burst:    float64(burst),
		interval: interval,
		buckets:  make(map[int32]*sendBucket),
	}
}

// allow consumes one send token for the user, refilling by elapsed time.
func (limiter *sendLimiter) allow(userID int32, now time.Time) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	bucket := limiter.buckets[userID]
	if bucket == nil {
		bucket = &sendBucket{tokens: limiter.burst, last: now}
		limiter.buckets[userID] = bucket
	}
	bucket.tokens += now.Sub(bucket.last).Seconds() / limiter.interval.Seconds()
	if bucket.tokens > limiter.burst {
		bucket.tokens = limiter.burst
	}
	bucket.last = now
	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens--
	return true
}

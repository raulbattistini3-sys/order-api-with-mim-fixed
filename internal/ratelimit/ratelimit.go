// Package ratelimit is the gateway's flood defence: a per-client token bucket.
//
// Idempotency stops duplicates from becoming duplicate orders, but it still runs
// the handler and touches the database for every request. A client that fires
// thousands of requests a second is a load problem on its own. The limiter caps
// how fast any one client can be served, so a flood from a single source is
// turned away with 429 before it reaches PocketBase.
package ratelimit

import (
	"math"
	"sync"
	"time"
)

// Limiter is a set of token buckets keyed by client. It is safe for concurrent
// use. A bucket refills at Rate tokens per second up to Burst tokens; each
// request spends one.
type Limiter struct {
	rate  float64 // tokens per second
	burst float64
	now   func() time.Time // injectable for tests

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

// New returns a Limiter allowing rate requests per second per client, with room
// for bursts of burst requests. A non-positive rate disables limiting.
func New(rate float64, burst int) *Limiter {
	return &Limiter{
		rate:    rate,
		burst:   float64(burst),
		now:     time.Now,
		buckets: make(map[string]*bucket),
	}
}

// Allow spends one token for key. It reports whether the request may proceed,
// and if not, how long to wait before a token is available.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	if l.rate <= 0 {
		return true, 0
	}
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b == nil {
		// A fresh client starts full, then spends one.
		l.buckets[key] = &bucket{tokens: l.burst - 1, last: now}
		return true, 0
	}
	b.tokens = math.Min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	// Round up, so Retry-After is never 0 while still denied.
	wait := time.Duration((1-b.tokens)/l.rate*float64(time.Second)) + time.Millisecond
	return false, wait
}

// gc drops buckets untouched for longer than ttl, so idle clients don't leak
// memory. A bucket is safe to drop once it has had time to refill completely,
// because a new one starts full too.
func (l *Limiter) gc(ttl time.Duration) {
	cutoff := l.now().Add(-ttl)
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, b := range l.buckets {
		if b.last.Before(cutoff) {
			delete(l.buckets, key)
		}
	}
}

// StartGC runs gc every interval until stop is closed. The caller owns stop.
func (l *Limiter) StartGC(interval time.Duration, stop <-chan struct{}) {
	ttl := interval
	if full := time.Duration(l.burst/math.Max(l.rate, 1e-9)) * time.Second; full > ttl {
		ttl = full
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				l.gc(ttl)
			case <-stop:
				return
			}
		}
	}()
}

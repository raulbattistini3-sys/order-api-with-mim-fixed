package ratelimit

import (
	"sync"
	"testing"
	"time"
)

func TestBurstThenRefill(t *testing.T) {
	now := time.Unix(0, 0)
	l := New(10, 5) // 10/s, burst 5
	l.now = func() time.Time { return now }

	for i := range 5 {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("request %d within burst denied", i)
		}
	}
	ok, retry := l.Allow("a")
	if ok {
		t.Fatal("6th request within burst allowed")
	}
	if retry <= 0 || retry > 200*time.Millisecond {
		t.Fatalf("retry-after = %v, want ~100ms", retry)
	}

	// After 100ms one token has refilled.
	now = now.Add(100 * time.Millisecond)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("request after refill denied")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("second request after one refill allowed")
	}
}

func TestClientsAreIndependent(t *testing.T) {
	now := time.Unix(0, 0)
	l := New(1, 1)
	l.now = func() time.Time { return now }

	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("a's first denied")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("a's second allowed")
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("b blocked by a's usage")
	}
}

func TestDisabled(t *testing.T) {
	l := New(0, 0)
	for range 1000 {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatal("disabled limiter denied a request")
		}
	}
}

func TestGCDropsIdle(t *testing.T) {
	now := time.Unix(0, 0)
	l := New(10, 5)
	l.now = func() time.Time { return now }
	l.Allow("a")
	now = now.Add(time.Minute)
	l.gc(time.Second)
	l.mu.Lock()
	n := len(l.buckets)
	l.mu.Unlock()
	if n != 0 {
		t.Fatalf("idle bucket not collected: %d left", n)
	}
}

func TestConcurrentAllowCountsExactly(t *testing.T) {
	now := time.Unix(0, 0)
	l := New(0.0001, 100) // effectively no refill during the test
	l.now = func() time.Time { return now }

	var granted int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range 500 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := l.Allow("a"); ok {
				mu.Lock()
				granted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if granted != 100 {
		t.Fatalf("granted %d, want exactly burst 100", granted)
	}
}

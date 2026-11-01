// Command loadgen is a load generator for profiling the gateway's defences. It
// is a benchmark for your own services, not an attack tool: it fires many signed
// POST /orders requests at a target you choose, with duplicates per logical
// order, and reports throughput, latency and the status mix. Point it at the
// gateway and at the API and compare.
//
//	go run ./cmd/loadgen -target http://localhost:8081 -orders 500 -dupes 5 -concurrency 100
//
// Expected against the gateway: one order per key (the rest 409 or replayed
// 201), and 429s once the per-IP rate limit is exceeded. Against the API
// directly: every request creates an order.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/you/orderguard/internal/signing"
)

func main() {
	target := flag.String("target", "http://localhost:8081", "base URL to load")
	secret := flag.String("secret", "dev-secret-change-me-0123456789", "signing secret (must match the gateway)")
	customer := flag.String("customer", "", "customer label; a timestamped one is generated if empty")
	orders := flag.Int("orders", 500, "number of distinct (idempotent) orders")
	dupes := flag.Int("dupes", 5, "identical copies fired per order")
	concurrency := flag.Int("concurrency", 100, "max requests in flight")
	timeout := flag.Duration("timeout", 15*time.Second, "per-request timeout")
	flag.Parse()

	if *customer == "" {
		*customer = fmt.Sprintf("load-%d", time.Now().UnixNano())
	}
	total := *orders * *dupes
	fmt.Printf("target=%s customer=%s orders=%d dupes=%d total=%d concurrency=%d\n",
		*target, *customer, *orders, *dupes, total, *concurrency)

	// Reuse connections so the generator measures the server, not the cost of a
	// fresh TCP+TLS handshake per request (and doesn't exhaust local ports).
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = *concurrency * 2
	transport.MaxIdleConnsPerHost = *concurrency
	client := &http.Client{Timeout: *timeout, Transport: transport}
	var (
		wg      sync.WaitGroup
		sem     = make(chan struct{}, *concurrency)
		mu      sync.Mutex
		latency = make([]time.Duration, 0, total)
		status  sync.Map // int -> *atomic.Int64
		replays atomic.Int64
	)
	bump := func(code int) {
		v, _ := status.LoadOrStore(code, new(atomic.Int64))
		v.(*atomic.Int64).Add(1)
	}

	start := time.Now()
	for i := range *orders {
		key := randKey()
		body := fmt.Appendf(nil, `{"customer":%q,"item":"load order %d","quantity":1}`, *customer, i)
		for range *dupes {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				code, replayed, d := fire(client, *target, *secret, key, body)
				bump(code)
				if replayed {
					replays.Add(1)
				}
				mu.Lock()
				latency = append(latency, d)
				mu.Unlock()
			}()
		}
	}
	wg.Wait()
	elapsed := time.Since(start)

	report(elapsed, total, latency, &status, replays.Load())
}

func fire(client *http.Client, target, secret, key string, body []byte) (int, bool, time.Duration) {
	const uri = "/orders"
	ts := fmt.Sprintf("%d", time.Now().UnixMilli())
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, target+uri, bytes.NewReader(body))
	if err != nil {
		return 0, false, 0
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(signing.HeaderKey, key)
	req.Header.Set(signing.HeaderTimestamp, ts)
	req.Header.Set(signing.HeaderSignature, signing.Sign([]byte(secret), http.MethodPost, uri, ts, key, body))

	begin := time.Now()
	res, err := client.Do(req)
	d := time.Since(begin)
	if err != nil {
		return 0, false, d
	}
	replayed := res.Header.Get(signing.HeaderKey) == "" && res.Header.Get("Idempotent-Replayed") == "true"
	res.Body.Close()
	return res.StatusCode, replayed, d
}

func report(elapsed time.Duration, total int, latency []time.Duration, status *sync.Map, replays int64) {
	slices.Sort(latency)
	pct := func(p float64) time.Duration {
		if len(latency) == 0 {
			return 0
		}
		i := int(p / 100 * float64(len(latency)))
		if i >= len(latency) {
			i = len(latency) - 1
		}
		return latency[i].Round(time.Microsecond)
	}

	fmt.Printf("\nelapsed      %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("throughput   %.0f req/s\n", float64(total)/elapsed.Seconds())
	fmt.Printf("latency      p50 %s  p95 %s  p99 %s  max %s\n", pct(50), pct(95), pct(99), pct(100))
	fmt.Printf("replayed     %d\n", replays)
	fmt.Printf("status\n")
	codes := map[int]int64{}
	status.Range(func(k, v any) bool { codes[k.(int)] = v.(*atomic.Int64).Load(); return true })
	for _, code := range sortedKeys(codes) {
		fmt.Printf("  %-3s %d\n", label(code), codes[code])
	}
}

func sortedKeys(m map[int]int64) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

func label(code int) string {
	if code == 0 {
		return "err"
	}
	return fmt.Sprintf("%d", code)
}

func randKey() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

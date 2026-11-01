// Command gateway protects the order API from duplicated, replayed and tampered
// requests. Clients talk to it instead of the API.
//
//	client ──▶ gateway :8081 ──▶ api :8080 ──▶ pocketbase
//	           rate limit
//	           signature
//	           idempotency
package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/you/orderguard/internal/gateway"
	"github.com/you/orderguard/internal/pocketbase"
	"github.com/you/orderguard/internal/ratelimit"
	"github.com/you/orderguard/internal/server"
	"github.com/you/orderguard/internal/signing"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("gateway stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	secret := os.Getenv("CLIENT_SECRET")
	if len(secret) < 16 {
		return errors.New("CLIENT_SECRET must be at least 16 characters")
	}
	maxAge, err := time.ParseDuration(server.Env("SIGNATURE_MAX_AGE", "30s"))
	if err != nil {
		return fmt.Errorf("SIGNATURE_MAX_AGE: %w", err)
	}
	upstream, err := url.Parse(server.Env("UPSTREAM", "http://localhost:8080"))
	if err != nil {
		return fmt.Errorf("UPSTREAM: %w", err)
	}

	rate, err := strconv.ParseFloat(server.Env("GATEWAY_RATE", "100"), 64)
	if err != nil {
		return fmt.Errorf("GATEWAY_RATE: %w", err)
	}
	burst, err := strconv.Atoi(server.Env("GATEWAY_BURST", "200"))
	if err != nil {
		return fmt.Errorf("GATEWAY_BURST: %w", err)
	}
	// Trust X-Forwarded-For only behind a proxy you control; otherwise a client
	// could forge it to dodge the per-IP limit.
	trustProxy := server.Env("TRUST_PROXY", "") == "1"

	pb := pocketbase.New(server.Env("PB_URL", "http://localhost:8090"), os.Getenv("PB_EMAIL"), os.Getenv("PB_PASSWORD"))
	verifier := signing.Verifier{Secret: []byte(secret), MaxAge: maxAge, Skew: 5 * time.Second}

	limiter := ratelimit.New(rate, burst)
	stop := make(chan struct{})
	defer close(stop)
	limiter.StartGC(time.Minute, stop)
	log.Info("rate limit", "per_ip_per_second", rate, "burst", burst, "trust_proxy", trustProxy)

	handler := gateway.New(verifier, pocketbase.NewIdempotencyStore(pb), pocketbase.NewAccounts(pb), limiter, trustProxy, upstream, log)

	return server.Run(":"+server.Env("PORT", "8081"), handler, log)
}

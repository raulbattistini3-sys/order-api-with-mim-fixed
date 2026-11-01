// Command gateway is the protective server. It verifies and de-duplicates
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
	"time"

	"github.com/you/orderguard/configs"
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
	cfg, err := configs.Load()
	if err != nil {
		return err
	}
	if len(cfg.ClientSecret) < 16 {
		return errors.New("CLIENT_SECRET must be at least 16 characters")
	}
	upstream, err := url.Parse(cfg.Gateway.Upstream)
	if err != nil {
		return fmt.Errorf("upstream: %w", err)
	}

	pb := pocketbase.New(cfg.PocketBase.URL, cfg.PocketBase.Email, cfg.PocketBase.Password)
	verifier := signing.Verifier{Secret: []byte(cfg.ClientSecret), MaxAge: cfg.Gateway.SignatureMaxAge, Skew: 5 * time.Second}
	limits := gateway.Limits{Body: cfg.Limits.BodyBytes(), Import: cfg.Limits.ImportBytes()}

	// Trust X-Forwarded-For only behind a proxy you control; otherwise a client
	// could forge it to dodge the per-IP limit.
	limiter := ratelimit.New(cfg.Gateway.Rate, cfg.Gateway.Burst)
	stop := make(chan struct{})
	defer close(stop)
	limiter.StartGC(time.Minute, stop)
	log.Info("rate limit", "per_ip_per_second", cfg.Gateway.Rate, "burst", cfg.Gateway.Burst, "trust_proxy", cfg.Gateway.TrustProxy)

	handler := gateway.New(verifier, limits, pocketbase.NewIdempotencyStore(pb), pocketbase.NewAccounts(pb), limiter, cfg.Gateway.TrustProxy, upstream, log)

	return server.Run(":"+cfg.Gateway.Port, handler, log)
}

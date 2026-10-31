// Command gateway protects the order API from duplicated, replayed and tampered
// requests. Clients talk to it instead of the API.
//
//	client ──▶ gateway :8081 ──▶ api :8080 ──▶ pocketbase
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

	"github.com/you/orderguard/internal/gateway"
	"github.com/you/orderguard/internal/pocketbase"
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

	pb := pocketbase.New(server.Env("PB_URL", "http://localhost:8090"), os.Getenv("PB_EMAIL"), os.Getenv("PB_PASSWORD"))
	verifier := signing.Verifier{Secret: []byte(secret), MaxAge: maxAge, Skew: 5 * time.Second}
	handler := gateway.New(verifier, pocketbase.NewIdempotencyStore(pb), upstream, log)

	return server.Run(":"+server.Env("PORT", "8081"), handler, log)
}

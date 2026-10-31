// Command api serves the order API on top of PocketBase. It trusts its callers:
// on its own it is the vulnerable baseline, and cmd/gateway is what protects it.
//
//	POST /orders
//	GET  /orders   ?customer=X → {count, orders}
//	GET  /healthz
package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/you/orderguard/internal/order"
	"github.com/you/orderguard/internal/pocketbase"
	"github.com/you/orderguard/internal/server"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	pb := pocketbase.New(server.Env("PB_URL", "http://localhost:8090"), os.Getenv("PB_EMAIL"), os.Getenv("PB_PASSWORD"))

	mux := http.NewServeMux()
	order.NewHandler(pocketbase.NewOrderRepository(pb), log).Register(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	if err := server.Run(":"+server.Env("PORT", "8080"), mux, log); err != nil {
		log.Error("api stopped", "err", err)
		os.Exit(1)
	}
}

// Command api serves the order API on top of PocketBase. It trusts its callers:
// on its own it is the vulnerable baseline, and cmd/gateway is what protects it.
//
//	POST /orders          GET /orders?customer=X
//	POST /auth/...        GET /auth/cnpj/{cnpj}
//	GET  /me              /me/orders, /me/todos
//	GET  /healthz
package main

import (
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/you/orderguard/internal/auth"
	"github.com/you/orderguard/internal/company"
	"github.com/you/orderguard/internal/order"
	"github.com/you/orderguard/internal/orderimport"
	"github.com/you/orderguard/internal/pocketbase"
	"github.com/you/orderguard/internal/server"
	"github.com/you/orderguard/internal/todo"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	pb := pocketbase.New(server.Env("PB_URL", "http://localhost:8090"), os.Getenv("PB_EMAIL"), os.Getenv("PB_PASSWORD"))
	directory := company.NewBrasilAPI(server.Env("CNPJ_API_URL", "https://brasilapi.com.br"))

	orders := pocketbase.NewOrderRepository(pb)
	importWorkers, err := strconv.Atoi(server.Env("IMPORT_WORKERS", "16"))
	if err != nil {
		log.Error("IMPORT_WORKERS", "err", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	order.NewHandler(orders, log).Register(mux)
	orderimport.NewHandler(orderimport.NewImporter(orders, importWorkers), log).Register(mux)
	todo.NewHandler(pocketbase.NewTodoRepository(pb), log).Register(mux)
	auth.NewHandler(pocketbase.NewAccounts(pb), pocketbase.NewCompanyRepository(pb), directory, log).Register(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	if err := server.Run(":"+server.Env("PORT", "8080"), mux, log); err != nil {
		log.Error("api stopped", "err", err)
		os.Exit(1)
	}
}

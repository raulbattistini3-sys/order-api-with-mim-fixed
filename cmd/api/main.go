// Command api serves the order API on top of PocketBase. It trusts its callers:
// on its own it is the vulnerable baseline, and cmd/gateway is what protects it.
//
//	POST /orders          GET /orders?customer=X
//	POST /auth/...        GET /auth/cnpj/{cnpj}
//	GET  /me              /me/orders, /me/orders/import, /me/todos
//	GET  /healthz
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/you/orderguard/configs"
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
	cfg, err := configs.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}
	pb := pocketbase.New(cfg.PocketBase.URL, cfg.PocketBase.Email, cfg.PocketBase.Password)
	directory := company.NewBrasilAPI(cfg.API.CNPJAPIURL)

	orders := pocketbase.NewOrderRepository(pb)
	importer := orderimport.NewImporter(orders, cfg.API.ImportWorkers)
	queue, err := orderimport.NewQueue(pocketbase.NewImportJobs(pb), importer, cfg.API.ImportSpoolDir, log)
	if err != nil {
		log.Error("import queue", "err", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	order.NewHandler(orders, log).Register(mux)
	orderimport.NewHandler(importer, queue, cfg.API.ImportSyncRows, cfg.Limits.ImportBytes(), log).Register(mux)
	todo.NewHandler(pocketbase.NewTodoRepository(pb), log).Register(mux)
	auth.NewHandler(pocketbase.NewAccounts(pb), pocketbase.NewCompanyRepository(pb), directory, log).Register(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	// Stopping mid-job is safe: the unfinished job resumes on the next start.
	ctx, stop := context.WithCancel(context.Background())
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		queue.Run(ctx)
	}()

	err = server.Run(":"+cfg.API.Port, mux, log)
	stop()
	<-drained
	if err != nil {
		log.Error("api stopped", "err", err)
		os.Exit(1)
	}
}

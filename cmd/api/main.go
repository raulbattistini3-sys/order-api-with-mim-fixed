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
	"os"

	"github.com/you/orderguard/configs"
	"github.com/you/orderguard/internal/api"
	"github.com/you/orderguard/internal/auth"
	"github.com/you/orderguard/internal/company"
	"github.com/you/orderguard/internal/order"
	"github.com/you/orderguard/internal/orderimport"
	"github.com/you/orderguard/internal/pocketbase"
	"github.com/you/orderguard/internal/search"
	"github.com/you/orderguard/internal/server"
	"github.com/you/orderguard/internal/todo"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("api stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := configs.Load()
	if err != nil {
		return err
	}
	pb := pocketbase.New(cfg.PocketBase.URL, cfg.PocketBase.Email, cfg.PocketBase.Password)
	orders := pocketbase.NewOrderRepository(pb)
	importer := orderimport.NewImporter(orders, cfg.API.ImportWorkers, cfg.API.ImportBatchSize)
	queue, err := orderimport.NewQueue(pocketbase.NewImportJobs(pb), importer, cfg.API.ImportSpoolDir, log)
	if err != nil {
		return err
	}

	// Search is optional, and the indexer runs apart from writes, so an outage of
	// Meilisearch can't take orders down with it.
	var searcher order.Searcher = order.NoSearch{}
	var indexer *search.Indexer
	if cfg.Search.URL != "" {
		client := search.New(cfg.Search.URL, cfg.Search.APIKey, cfg.Search.Index)
		searcher = client
		indexer = search.NewIndexer(client, orders, log)
	}

	handler := api.New(api.Deps{
		Accounts:       auth.NewService(pocketbase.NewAccounts(pb), pocketbase.NewCompanyRepository(pb), company.NewBrasilAPI(cfg.API.CNPJAPIURL), log),
		Orders:         order.NewService(orders, searcher),
		Imports:        orderimport.NewService(importer, queue, cfg.API.ImportSyncRows),
		Todos:          todo.NewService(pocketbase.NewTodoRepository(pb)),
		MaxImportBytes: cfg.Limits.ImportBytes(),
		Log:            log,
	})

	// Stopping mid-job is safe: the unfinished job resumes on the next start.
	ctx, stop := context.WithCancel(context.Background())
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		queue.Run(ctx)
	}()
	if indexer != nil {
		go indexer.Run(ctx)
	}

	err = server.Run(":"+cfg.API.Port, handler, log)
	stop()
	<-drained
	return err
}

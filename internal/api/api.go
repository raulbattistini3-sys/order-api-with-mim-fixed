// Package api assembles the order API. It lives here rather than in cmd/api so
// tests can run the same routes a real server does, with any store behind them.
package api

import (
	"log/slog"
	"net/http"

	"github.com/you/orderguard/internal/auth"
	"github.com/you/orderguard/internal/order"
	"github.com/you/orderguard/internal/orderimport"
	"github.com/you/orderguard/internal/todo"
)

// Deps is everything the API needs from outside.
type Deps struct {
	Accounts       auth.Service
	Orders         order.Service
	Imports        orderimport.Service
	Todos          todo.Service
	MaxImportBytes int64
	Log            *slog.Logger
}

// New returns the API's handler.
func New(d Deps) http.Handler {
	mux := http.NewServeMux()
	order.Routes(mux, d.Orders, d.Log)
	orderimport.Routes(mux, d.Imports, d.MaxImportBytes, d.Log)
	todo.Routes(mux, d.Todos, d.Log)
	auth.Routes(mux, d.Accounts, d.Log)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	return mux
}

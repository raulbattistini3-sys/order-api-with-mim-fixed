// Package gateway is the server that stops the attacks. It sits in front of the
// order API: every order must pass signature verification, then idempotency,
// before it is forwarded.
package gateway

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/you/orderguard/internal/httpjson"
	"github.com/you/orderguard/internal/idempotency"
	"github.com/you/orderguard/internal/order"
	"github.com/you/orderguard/internal/signing"
)

// lockTimeout must outlast a forwarded request.
const lockTimeout = 30 * time.Second

// New returns the gateway handler, forwarding accepted requests to upstream.
func New(verifier signing.Verifier, store idempotency.Store, upstream *url.URL, log *slog.Logger) http.Handler {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(upstream)
			// Set here, after verification, so a client can't supply its own.
			r.Out.Header.Set(order.RefHeader, r.In.Header.Get(signing.HeaderKey))
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			log.Error("upstream", "err", err)
			httpjson.Error(w, http.StatusBadGateway, "upstream unavailable")
		},
	}
	idem := idempotency.Middleware(store, lockTimeout, log)

	mux := http.NewServeMux()
	mux.Handle("POST /orders", verifier.Middleware(idem(proxy)))
	mux.Handle("GET /orders", proxy)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	return mux
}

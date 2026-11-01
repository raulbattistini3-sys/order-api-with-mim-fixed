// Package gateway is the server that stops the attacks. It sits in front of the
// order API: a per-client rate limit guards the door, then every request must
// pass signature verification, and each route adds the session and idempotency
// checks it needs before it is forwarded.
package gateway

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/you/orderguard/internal/auth"
	"github.com/you/orderguard/internal/httpjson"
	"github.com/you/orderguard/internal/idempotency"
	"github.com/you/orderguard/internal/order"
	"github.com/you/orderguard/internal/ratelimit"
	"github.com/you/orderguard/internal/signing"
)

// lockTimeout must outlast a forwarded request.
const lockTimeout = 30 * time.Second

// Limits caps request bodies, in bytes. Signature verification reads the body
// before it knows who sent it, so only the import route gets the larger cap.
type Limits struct {
	Body   int64
	Import int64
}

// New returns the gateway handler, forwarding accepted requests to upstream.
// limiter throttles per client IP; pass a disabled limiter to turn it off.
func New(verifier signing.Verifier, limits Limits, store idempotency.Store, sessions auth.Verifier, limiter *ratelimit.Limiter, trustProxy bool, upstream *url.URL, log *slog.Logger) http.Handler {
	// The default transport keeps only 2 idle connections per host, so under
	// load the gateway opens and discards a socket per request and exhausts the
	// ephemeral port range. Keep a generous pool so connections to the API are
	// reused.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 512
	transport.MaxIdleConnsPerHost = 256

	proxy := &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(upstream)
			// Set here, after verification, so a client can't supply its own.
			r.Out.Header.Set(order.RefHeader, r.In.Header.Get(signing.HeaderKey))
			r.Out.Header.Del(auth.UserHeader)
			r.Out.Header.Del(auth.CompanyHeader)
			r.Out.Header.Del("Authorization")
			if id, ok := auth.IdentityFrom(r.In.Context()); ok {
				r.Out.Header.Set(auth.UserHeader, id.UserID)
				r.Out.Header.Set(auth.CompanyHeader, id.CompanyID)
			}
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			log.Error("upstream", "err", err)
			httpjson.Error(w, http.StatusBadGateway, "upstream unavailable")
		},
	}
	verifier.MaxBody = limits.Body
	signed := verifier.Middleware
	uploadVerifier := verifier
	uploadVerifier.MaxBody = limits.Import
	session := auth.RequireSession(sessions)
	idem := idempotency.Middleware(store, lockTimeout, limits.Body, log)

	mux := http.NewServeMux()
	mux.Handle("POST /orders", signed(idem(proxy)))
	// Auth routes skip idempotency on purpose: it stores response bodies, and
	// these contain session tokens.
	mux.Handle("/auth/", signed(proxy))
	mux.Handle("POST /me/orders", signed(session(idem(proxy))))
	// Import carries a whole file and dedupes per row in the store, so it skips
	// the per-request idempotency layer.
	mux.Handle("POST /me/orders/import", uploadVerifier.Middleware(session(proxy)))
	mux.Handle("/me", signed(session(proxy)))
	mux.Handle("/me/", signed(session(proxy)))

	limited := limiter.Middleware(ratelimit.IPKey(trustProxy))(mux)
	// Health checks bypass the limit, so probes can't be throttled.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		limited.ServeHTTP(w, r)
	})
}

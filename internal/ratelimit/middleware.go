package ratelimit

import (
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/you/orderguard/internal/httpjson"
)

// ClientIP returns the remote IP for r. It trusts X-Forwarded-For only when
// trustProxy is set, since a client can forge that header otherwise.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			// The left-most entry is the original client.
			return strings.TrimSpace(strings.SplitN(fwd, ",", 2)[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Middleware rejects requests from a client that is over its rate with 429 and
// a Retry-After header. key picks what to limit on (usually the client IP).
func (l *Limiter) Middleware(key func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ok, retry := l.Allow(key(r))
			if !ok {
				w.Header().Set("Retry-After", strconv.Itoa(max(int(retry.Seconds()), 1)))
				httpjson.Error(w, http.StatusTooManyRequests, "rate limit exceeded; slow down")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// IPKey limits per client IP.
func IPKey(trustProxy bool) func(*http.Request) string {
	return func(r *http.Request) string { return ClientIP(r, trustProxy) }
}

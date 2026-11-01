package gateway

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/you/orderguard/internal/auth"
	"github.com/you/orderguard/internal/idempotency"
	"github.com/you/orderguard/internal/ratelimit"
	"github.com/you/orderguard/internal/signing"
)

// Never reached: none of these tests send a route that uses idempotency twice.
type noStore struct{ idempotency.Store }

func (noStore) Insert(context.Context, string, string) (string, error) { return "1", nil }
func (noStore) Complete(context.Context, string, int, []byte) error    { return nil }

type sessions struct{}

func (sessions) Verify(_ context.Context, token string) (auth.User, error) {
	if token != "good" {
		return auth.User{}, auth.ErrInvalidToken
	}
	return auth.User{ID: "u1", Company: "c1", Active: true}, nil
}

var secret = []byte("test-secret-0123456789")

func setup(t *testing.T, upstream http.HandlerFunc) *httptest.Server {
	t.Helper()
	api := httptest.NewServer(upstream)
	t.Cleanup(api.Close)
	u, _ := url.Parse(api.URL)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	limiter := ratelimit.New(0, 0) // disabled; the limiter has its own tests
	h := New(signing.Verifier{Secret: secret, MaxAge: time.Minute}, Limits{Body: 1 << 10, Import: 1 << 11}, noStore{}, sessions{}, limiter, false, u, log)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func signed(t *testing.T, srv *httptest.Server, method, uri, body string, headers map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+uri, strings.NewReader(body))
	ts, key := strconv.FormatInt(time.Now().UnixMilli(), 10), "key-00000001"
	req.Header.Set(signing.HeaderKey, key)
	req.Header.Set(signing.HeaderTimestamp, ts)
	req.Header.Set(signing.HeaderSignature, signing.Sign(secret, method, uri, ts, key, []byte(body)))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res
}

func TestBodyLimits(t *testing.T) {
	srv := setup(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	session := map[string]string{"Authorization": "Bearer good"}

	for _, tt := range []struct {
		name string
		uri  string
		size int
		want int
	}{
		{"default route at limit", "/auth/login", 1 << 10, http.StatusOK},
		{"default route over limit", "/auth/login", 1<<10 + 1, http.StatusRequestEntityTooLarge},
		{"import over default limit", "/me/orders/import", 1 << 11, http.StatusOK},
		{"import over its limit", "/me/orders/import", 1<<11 + 1, http.StatusRequestEntityTooLarge},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res := signed(t, srv, http.MethodPost, tt.uri, strings.Repeat("x", tt.size), session)
			if res.StatusCode != tt.want {
				t.Fatalf("status = %d, want %d", res.StatusCode, tt.want)
			}
		})
	}
}

func TestSessionRouteForwardsVerifiedIdentityOnly(t *testing.T) {
	var got http.Header
	srv := setup(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusCreated)
	})

	res := signed(t, srv, "POST", "/me/orders", `{"item":"x","quantity":1}`, map[string]string{
		"Authorization":    "Bearer good",
		auth.UserHeader:    "someone-else",
		auth.CompanyHeader: "another-company",
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if got.Get(auth.UserHeader) != "u1" || got.Get(auth.CompanyHeader) != "c1" {
		t.Fatalf("identity headers = %q, %q", got.Get(auth.UserHeader), got.Get(auth.CompanyHeader))
	}
	if got.Get("X-Order-Ref") != "u1:key-00000001" {
		t.Fatalf("order ref = %q", got.Get("X-Order-Ref"))
	}
	if got.Get("Authorization") != "" {
		t.Fatal("session token was forwarded to the API")
	}
}

func TestRejectedRequestsNeverReachTheAPI(t *testing.T) {
	srv := setup(t, func(http.ResponseWriter, *http.Request) { t.Error("request reached the API") })

	if res := signed(t, srv, "GET", "/me", "", nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no session: status = %d", res.StatusCode)
	}
	if res := signed(t, srv, "GET", "/me", "", map[string]string{"Authorization": "Bearer bad"}); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad session: status = %d", res.StatusCode)
	}
	res, err := http.Post(srv.URL+"/auth/login", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unsigned: status = %d", res.StatusCode)
	}
	if res := signed(t, srv, "GET", "/orders?customer=x", "", nil); res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("listing orders by name through the gateway: status = %d", res.StatusCode)
	}
}

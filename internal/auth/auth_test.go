package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/you/orderguard/internal/idempotency"
)

type verifierFunc func(ctx context.Context, token string) (User, error)

func (f verifierFunc) Verify(ctx context.Context, token string) (User, error) { return f(ctx, token) }

func TestRequireSession(t *testing.T) {
	verifier := verifierFunc(func(_ context.Context, token string) (User, error) {
		switch token {
		case "good":
			return User{ID: "u1", Company: "c1", Active: true}, nil
		case "inactive":
			return User{ID: "u2", Company: "c1"}, nil
		}
		return User{}, ErrInvalidToken
	})

	tests := []struct {
		name   string
		header string
		want   int
	}{
		{"valid", "Bearer good", http.StatusNoContent},
		{"no header", "", http.StatusUnauthorized},
		{"wrong scheme", "Basic good", http.StatusUnauthorized},
		{"bad token", "Bearer nope", http.StatusUnauthorized},
		{"inactive user", "Bearer inactive", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var key string
			var id Identity
			h := RequireSession(verifier)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				key = r.Header.Get(idempotency.Header)
				id, _ = IdentityFrom(r.Context())
				w.WriteHeader(http.StatusNoContent)
			}))
			r := httptest.NewRequest("POST", "/me/orders", nil)
			r.Header.Set(idempotency.Header, "key-00000001")
			if tt.header != "" {
				r.Header.Set("Authorization", tt.header)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d", w.Code, tt.want)
			}
			if tt.want == http.StatusNoContent && (key != "u1:key-00000001" || id != (Identity{UserID: "u1", CompanyID: "c1"})) {
				t.Fatalf("key = %q, identity = %+v", key, id)
			}
		})
	}
}

func TestRequireIdentityRejectsMissingHeader(t *testing.T) {
	h := RequireIdentity(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal(errors.New("handler ran without an identity"))
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/me", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

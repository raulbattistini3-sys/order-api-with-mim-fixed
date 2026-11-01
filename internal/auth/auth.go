// Package auth handles accounts: registering, logging in, resetting passwords,
// and proving who a request belongs to.
//
// The gateway verifies the session token and passes the result to the API as
// UserHeader and CompanyHeader. The API trusts those headers, so its port must
// stay private.
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/you/orderguard/internal/httpjson"
	"github.com/you/orderguard/internal/idempotency"
)

// Headers the gateway sets on forwarded requests.
const (
	UserHeader    = "X-User-Id"
	CompanyHeader = "X-Company-Id"
)

var (
	// ErrInvalidCredentials means a wrong email or password.
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	// ErrInvalidToken means a session or reset token was rejected.
	ErrInvalidToken = errors.New("auth: invalid token")
	// ErrEmailTaken means an account with this email already exists.
	ErrEmailTaken = errors.New("auth: email already registered")
	// ErrWeakPassword means the store rejected the password.
	ErrWeakPassword = errors.New("auth: password rejected")
)

// User is one account. Permission is ADM, Estoque or SU.
type User struct {
	ID          string   `json:"id,omitempty"`
	Email       string   `json:"email"`
	Name        string   `json:"name"`
	Permission  string   `json:"permission"`
	Active      bool     `json:"active"`
	Company     string   `json:"company"`
	Phone       string   `json:"phone"` // E.164, such as +5511987654321
	JobFunction string   `json:"job_function"`
	JobTitle    string   `json:"job_title"`
	Seniority   string   `json:"seniority"`
	BuyingRoles []string `json:"buying_roles"`
}

// Session is a logged-in user and their token.
type Session struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

// Verifier resolves a session token to its user.
type Verifier interface {
	Verify(ctx context.Context, token string) (User, error)
}

// Accounts stores users and owns their credentials.
type Accounts interface {
	Verifier
	Create(ctx context.Context, u User, password string) (User, error)
	Delete(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (User, error)
	Login(ctx context.Context, email, password string) (Session, error)
	RequestPasswordReset(ctx context.Context, email string) error
	ConfirmPasswordReset(ctx context.Context, token, password string) error
}

// Identity says who a verified request belongs to.
type Identity struct {
	UserID    string
	CompanyID string
}

type ctxKey struct{}

// WithIdentity returns ctx carrying id.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// IdentityFrom returns the identity in ctx, if there is one.
func IdentityFrom(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}

// RequireSession is the gateway's check. It rejects requests without a valid
// session for an active user, and scopes the Idempotency-Key to the user, so
// one user's key can never return another user's cached response.
func RequireSession(v Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || token == "" {
				httpjson.Error(w, http.StatusUnauthorized, "session required")
				return
			}
			u, err := v.Verify(r.Context(), token)
			if err != nil || !u.Active {
				httpjson.Error(w, http.StatusUnauthorized, "invalid session")
				return
			}
			if key := r.Header.Get(idempotency.Header); key != "" {
				r.Header.Set(idempotency.Header, u.ID+":"+key)
			}
			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), Identity{UserID: u.ID, CompanyID: u.Company})))
		})
	}
}

// RequireIdentity is the API's check. It reads the gateway's headers.
func RequireIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := r.Header.Get(UserHeader)
		if user == "" {
			httpjson.Error(w, http.StatusUnauthorized, "session required")
			return
		}
		id := Identity{UserID: user, CompanyID: r.Header.Get(CompanyHeader)}
		next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
	})
}

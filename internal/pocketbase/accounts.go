package pocketbase

import (
	"context"
	"errors"
	"net/http"

	"github.com/you/orderguard/internal/auth"
)

const usersCollection = "users"

// Accounts implements auth.Accounts on the users auth collection.
type Accounts struct {
	pb *Client
}

// NewAccounts returns an Accounts using pb.
func NewAccounts(pb *Client) *Accounts {
	return &Accounts{pb: pb}
}

type userRecord struct {
	auth.User
	Password        string `json:"password,omitempty"`
	PasswordConfirm string `json:"passwordConfirm,omitempty"`
}

type authResponse struct {
	Token  string    `json:"token"`
	Record auth.User `json:"record"`
}

// Create inserts a user.
func (a *Accounts) Create(ctx context.Context, u auth.User, password string) (auth.User, error) {
	var out auth.User
	err := a.pb.Create(ctx, usersCollection, userRecord{User: u, Password: password, PasswordConfirm: password}, &out)
	var apiErr *APIError
	switch {
	case errors.Is(err, ErrNotUnique):
		return auth.User{}, auth.ErrEmailTaken
	case errors.As(err, &apiErr) && apiErr.Data["password"] != nil:
		return auth.User{}, auth.ErrWeakPassword
	}
	return out, err
}

// Delete removes a user.
func (a *Accounts) Delete(ctx context.Context, id string) error {
	return a.pb.Delete(ctx, usersCollection, id)
}

// Get returns a user by id.
func (a *Accounts) Get(ctx context.Context, id string) (auth.User, error) {
	var out auth.User
	err := a.pb.Get(ctx, usersCollection, id, &out)
	return out, err
}

// Login exchanges an email and password for a session.
func (a *Accounts) Login(ctx context.Context, email, password string) (auth.Session, error) {
	var res authResponse
	in := map[string]string{"identity": email, "password": password}
	err := a.pb.Public(ctx, http.MethodPost, authPath("auth-with-password"), "", in, &res)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status < http.StatusInternalServerError {
		return auth.Session{}, auth.ErrInvalidCredentials
	}
	if err != nil {
		return auth.Session{}, err
	}
	return auth.Session{Token: res.Token, User: res.Record}, nil
}

// Verify asks PocketBase whether token is still good. Tokens can't be checked
// locally because each is signed with a per-user key, and asking means a
// password change ends older sessions at once.
func (a *Accounts) Verify(ctx context.Context, token string) (auth.User, error) {
	var res authResponse
	err := a.pb.Public(ctx, http.MethodPost, authPath("auth-refresh"), token, nil, &res)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status < http.StatusInternalServerError {
		return auth.User{}, auth.ErrInvalidToken
	}
	return res.Record, err
}

// RequestPasswordReset emails a reset link. PocketBase answers the same for
// unknown addresses.
func (a *Accounts) RequestPasswordReset(ctx context.Context, email string) error {
	return a.pb.Public(ctx, http.MethodPost, authPath("request-password-reset"), "", map[string]string{"email": email}, nil)
}

// ConfirmPasswordReset sets a new password using a token from the reset email.
func (a *Accounts) ConfirmPasswordReset(ctx context.Context, token, password string) error {
	in := map[string]string{"token": token, "password": password, "passwordConfirm": password}
	err := a.pb.Public(ctx, http.MethodPost, authPath("confirm-password-reset"), "", in, nil)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status < http.StatusInternalServerError {
		return auth.ErrInvalidToken
	}
	return err
}

func authPath(action string) string {
	return "/api/collections/" + usersCollection + "/" + action
}

package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/you/orderguard/internal/company"
)

// stubService answers every call with err, to test the error mapping alone.
type stubService struct {
	Service
	err error
}

func (s stubService) Register(context.Context, RegisterInput) (Session, error) {
	return Session{}, s.err
}

func (s stubService) Login(context.Context, string, string) (Session, error) {
	return Session{}, s.err
}

func (s stubService) ForgotPassword(context.Context, string) {}

func post(t *testing.T, svc Service, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	Routes(mux, svc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	return rec
}

func TestRegisterStatuses(t *testing.T) {
	for _, tt := range []struct {
		err  error
		want int
	}{
		{nil, http.StatusCreated},
		{ErrInvalidRegistration, http.StatusBadRequest},
		{company.ErrNotFound, http.StatusUnprocessableEntity},
		{ErrCompanyUnavailable, http.StatusBadGateway},
		{company.ErrDuplicate, http.StatusConflict},
		{ErrEmailTaken, http.StatusConflict},
		{errors.New("boom"), http.StatusInternalServerError},
	} {
		if rec := post(t, stubService{err: tt.err}, "/auth/register", `{"name":"Ana"}`); rec.Code != tt.want {
			t.Errorf("%v: status = %d, want %d", tt.err, rec.Code, tt.want)
		}
	}
}

func TestInternalErrorsAreNotShownToClients(t *testing.T) {
	rec := post(t, stubService{err: errors.New("pocketbase: secret detail")}, "/auth/login", `{"email":"a"}`)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestForgotPasswordAlwaysAnswers204(t *testing.T) {
	if rec := post(t, stubService{}, "/auth/forgot-password", `{"email":"nobody@example.com"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestRejectsUnknownFields(t *testing.T) {
	if rec := post(t, stubService{}, "/auth/register", `{"permission":"SU"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

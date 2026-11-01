package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/you/orderguard/internal/company"
	"github.com/you/orderguard/internal/httpjson"
)

// Routes adds the account endpoints to mux.
func Routes(mux *http.ServeMux, svc Service, log *slog.Logger) {
	mux.Handle("POST /auth/register", register(svc, log))
	mux.Handle("POST /auth/login", login(svc, log))
	mux.Handle("POST /auth/forgot-password", forgotPassword(svc))
	mux.Handle("POST /auth/reset-password", resetPassword(svc, log))
	mux.Handle("GET /auth/cnpj/{cnpj}", lookupCNPJ(svc, log))
	mux.Handle("GET /me", RequireIdentity(me(svc, log)))
}

func register(svc Service, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in RegisterInput
		if !decode(w, r, &in) {
			return
		}
		session, err := svc.Register(r.Context(), in)
		switch {
		case err == nil:
			httpjson.Write(w, http.StatusCreated, session)
		case errors.Is(err, ErrInvalidRegistration), errors.Is(err, ErrInvalidProfile):
			httpjson.Error(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, company.ErrNotFound):
			httpjson.Error(w, http.StatusUnprocessableEntity, "CNPJ not found")
		case errors.Is(err, ErrCompanyUnavailable):
			httpjson.Error(w, http.StatusBadGateway, "company data unavailable; provide razao_social")
		case errors.Is(err, company.ErrDuplicate):
			httpjson.Error(w, http.StatusConflict, "CNPJ already registered")
		case errors.Is(err, ErrEmailTaken):
			httpjson.Error(w, http.StatusConflict, "email already registered")
		case errors.Is(err, ErrWeakPassword):
			httpjson.Error(w, http.StatusBadRequest, "password rejected")
		default:
			log.Error("register", "err", err)
			httpjson.Error(w, http.StatusInternalServerError, "internal error")
		}
	}
}

func login(svc Service, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if !decode(w, r, &req) {
			return
		}
		session, err := svc.Login(r.Context(), req.Email, req.Password)
		switch {
		case errors.Is(err, ErrInvalidCredentials):
			httpjson.Error(w, http.StatusUnauthorized, "invalid email or password")
		case err != nil:
			log.Error("login", "err", err)
			httpjson.Error(w, http.StatusInternalServerError, "internal error")
		default:
			httpjson.Write(w, http.StatusOK, session)
		}
	}
}

// forgotPassword always answers 204, so it can't reveal which emails exist.
func forgotPassword(svc Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email string `json:"email"`
		}
		if !decode(w, r, &req) {
			return
		}
		svc.ForgotPassword(r.Context(), req.Email)
		w.WriteHeader(http.StatusNoContent)
	}
}

func resetPassword(svc Service, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Token    string `json:"token"`
			Password string `json:"password"`
		}
		if !decode(w, r, &req) {
			return
		}
		switch err := svc.ResetPassword(r.Context(), req.Token, req.Password); {
		case errors.Is(err, ErrInvalidReset):
			httpjson.Error(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrInvalidToken):
			httpjson.Error(w, http.StatusBadRequest, "invalid or expired link")
		case err != nil:
			log.Error("confirm password reset", "err", err)
			httpjson.Error(w, http.StatusInternalServerError, "internal error")
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

func lookupCNPJ(svc Service, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := svc.LookupCNPJ(r.Context(), r.PathValue("cnpj"))
		switch {
		case errors.Is(err, ErrInvalidCNPJ):
			httpjson.Error(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, company.ErrNotFound):
			httpjson.Error(w, http.StatusNotFound, "CNPJ not found")
		case errors.Is(err, ErrCompanyUnavailable):
			httpjson.Error(w, http.StatusBadGateway, err.Error())
		case err != nil:
			log.Error("cnpj lookup", "err", err)
			httpjson.Error(w, http.StatusInternalServerError, "internal error")
		default:
			httpjson.Write(w, http.StatusOK, c)
		}
	}
}

func me(svc Service, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, _ := IdentityFrom(r.Context())
		u, c, err := svc.Me(r.Context(), id.UserID)
		if err != nil {
			log.Error("get account", "err", err)
			httpjson.Error(w, http.StatusInternalServerError, "internal error")
			return
		}
		httpjson.Write(w, http.StatusOK, map[string]any{"user": u, "company": c})
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		httpjson.Error(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

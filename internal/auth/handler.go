package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"

	"github.com/you/orderguard/internal/company"
	"github.com/you/orderguard/internal/httpjson"
)

// Password length limits. bcrypt ignores anything past 72 bytes.
const (
	minPassword = 10
	maxPassword = 72
)

// Handler serves the account endpoints.
type Handler struct {
	accounts  Accounts
	companies company.Repository
	directory company.Directory
	log       *slog.Logger
}

// NewHandler returns a Handler with its dependencies.
func NewHandler(accounts Accounts, companies company.Repository, directory company.Directory, log *slog.Logger) *Handler {
	return &Handler{accounts: accounts, companies: companies, directory: directory, log: log}
}

// Register adds the account routes to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /auth/register", h.register)
	mux.HandleFunc("POST /auth/login", h.login)
	mux.HandleFunc("POST /auth/forgot-password", h.forgotPassword)
	mux.HandleFunc("POST /auth/reset-password", h.resetPassword)
	mux.HandleFunc("GET /auth/cnpj/{cnpj}", h.lookupCNPJ)
	mux.Handle("GET /me", RequireIdentity(http.HandlerFunc(h.me)))
}

type registerRequest struct {
	Name        string   `json:"name"`
	Email       string   `json:"email"`
	Password    string   `json:"password"`
	Phone       string   `json:"phone"`
	JobFunction string   `json:"job_function"`
	JobTitle    string   `json:"job_title"`
	Seniority   string   `json:"seniority"`
	BuyingRoles []string `json:"buying_roles"`
	CNPJ        string   `json:"cnpj"`
	RazaoSocial string   `json:"razao_social"` // used only if the registry is unavailable
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !decode(w, r, &req) {
		return
	}
	req.Name, req.Email = strings.TrimSpace(req.Name), strings.TrimSpace(req.Email)
	req.RazaoSocial = strings.TrimSpace(req.RazaoSocial)
	cnpj := company.NormalizeCNPJ(req.CNPJ)
	if req.Name == "" || !validEmail(req.Email) || !validPassword(req.Password) || !company.ValidCNPJ(cnpj) {
		httpjson.Error(w, http.StatusBadRequest, "name, a valid email, a valid CNPJ and a password of 10-72 characters are required")
		return
	}
	if !validProfile(req.Phone, req.JobFunction, req.BuyingRoles) {
		httpjson.Error(w, http.StatusBadRequest, "phone must be E.164, and job_function and buying_roles must be known values")
		return
	}
	if req.BuyingRoles == nil {
		req.BuyingRoles = []string{}
	}

	c, err := h.resolveCompany(r.Context(), cnpj, req.RazaoSocial)
	switch {
	case errors.Is(err, company.ErrNotFound):
		httpjson.Error(w, http.StatusUnprocessableEntity, "CNPJ not found")
		return
	case err != nil:
		httpjson.Error(w, http.StatusBadGateway, "company data unavailable; provide razao_social")
		return
	}
	c, err = h.companies.Create(r.Context(), c)
	switch {
	case errors.Is(err, company.ErrDuplicate):
		httpjson.Error(w, http.StatusConflict, "CNPJ already registered")
		return
	case err != nil:
		h.log.Error("create company", "err", err)
		httpjson.Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Whoever registers a company administers it. A client can't choose this.
	u, err := h.accounts.Create(r.Context(), User{
		Email:       req.Email,
		Name:        req.Name,
		Permission:  "ADM",
		Active:      true,
		Company:     c.ID,
		Phone:       req.Phone,
		JobFunction: req.JobFunction,
		JobTitle:    strings.TrimSpace(req.JobTitle),
		Seniority:   strings.TrimSpace(req.Seniority),
		BuyingRoles: req.BuyingRoles,
	}, req.Password)
	if err != nil {
		if delErr := h.companies.Delete(r.Context(), c.ID); delErr != nil {
			h.log.Error("remove company after failed registration", "company", c.ID, "err", delErr)
		}
		switch {
		case errors.Is(err, ErrEmailTaken):
			httpjson.Error(w, http.StatusConflict, "email already registered")
		case errors.Is(err, ErrWeakPassword):
			httpjson.Error(w, http.StatusBadRequest, "password rejected")
		default:
			h.log.Error("create user", "err", err)
			httpjson.Error(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	s, err := h.accounts.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		h.log.Error("login after registration", "user", u.ID, "err", err)
		httpjson.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpjson.Write(w, http.StatusCreated, s)
}

// resolveCompany prefers the registry. If it is down, registration can still
// proceed with the name the user typed.
func (h *Handler) resolveCompany(ctx context.Context, cnpj, razaoSocial string) (company.Company, error) {
	c, err := h.directory.Lookup(ctx, cnpj)
	if err == nil || errors.Is(err, company.ErrNotFound) {
		return c, err
	}
	h.log.Warn("cnpj lookup", "err", err)
	if razaoSocial == "" {
		return company.Company{}, err
	}
	return company.Company{CNPJ: cnpj, RazaoSocial: razaoSocial, CNAEsSecundarios: []company.CNAE{}}, nil
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	s, err := h.accounts.Login(r.Context(), strings.TrimSpace(req.Email), req.Password)
	switch {
	case errors.Is(err, ErrInvalidCredentials):
		httpjson.Error(w, http.StatusUnauthorized, "invalid email or password")
	case err != nil:
		h.log.Error("login", "err", err)
		httpjson.Error(w, http.StatusInternalServerError, "internal error")
	case !s.User.Active:
		httpjson.Error(w, http.StatusUnauthorized, "invalid email or password")
	default:
		httpjson.Write(w, http.StatusOK, s)
	}
}

// forgotPassword always answers 204, so it can't reveal which emails exist.
func (h *Handler) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if !decode(w, r, &req) {
		return
	}
	if email := strings.TrimSpace(req.Email); validEmail(email) {
		if err := h.accounts.RequestPasswordReset(r.Context(), email); err != nil {
			h.log.Error("request password reset", "err", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Token == "" || !validPassword(req.Password) {
		httpjson.Error(w, http.StatusBadRequest, "token and a password of 10-72 characters are required")
		return
	}
	switch err := h.accounts.ConfirmPasswordReset(r.Context(), req.Token, req.Password); {
	case errors.Is(err, ErrInvalidToken):
		httpjson.Error(w, http.StatusBadRequest, "invalid or expired link")
	case err != nil:
		h.log.Error("confirm password reset", "err", err)
		httpjson.Error(w, http.StatusInternalServerError, "internal error")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *Handler) lookupCNPJ(w http.ResponseWriter, r *http.Request) {
	cnpj := company.NormalizeCNPJ(r.PathValue("cnpj"))
	if !company.ValidCNPJ(cnpj) {
		httpjson.Error(w, http.StatusBadRequest, "invalid CNPJ")
		return
	}
	c, err := h.directory.Lookup(r.Context(), cnpj)
	switch {
	case errors.Is(err, company.ErrNotFound):
		httpjson.Error(w, http.StatusNotFound, "CNPJ not found")
	case err != nil:
		h.log.Warn("cnpj lookup", "err", err)
		httpjson.Error(w, http.StatusBadGateway, "company data unavailable")
	default:
		httpjson.Write(w, http.StatusOK, c)
	}
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	id, _ := IdentityFrom(r.Context())
	u, err := h.accounts.Get(r.Context(), id.UserID)
	if err != nil {
		h.log.Error("get user", "err", err)
		httpjson.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	c, err := h.companies.Get(r.Context(), u.Company)
	if err != nil {
		h.log.Error("get company", "err", err)
		httpjson.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"user": u, "company": c})
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

func validEmail(s string) bool {
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s
}

func validPassword(s string) bool {
	return len(s) >= minPassword && len(s) <= maxPassword
}

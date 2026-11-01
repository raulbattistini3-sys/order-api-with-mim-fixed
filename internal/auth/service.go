package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/mail"
	"strings"

	"github.com/you/orderguard/internal/company"
)

// Password length limits. bcrypt ignores anything past 72 bytes.
const (
	minPassword = 10
	maxPassword = 72
)

var (
	// ErrInvalidRegistration means a required field is missing or malformed.
	ErrInvalidRegistration = errors.New("name, a valid email, a valid CNPJ and a password of 10-72 characters are required")
	// ErrInvalidProfile means an optional profile field holds an unknown value.
	ErrInvalidProfile = errors.New("phone must be E.164, and job_function and buying_roles must be known values")
	// ErrInvalidReset means a password reset is missing its token or password.
	ErrInvalidReset = errors.New("token and a password of 10-72 characters are required")
	// ErrInvalidCNPJ means a CNPJ failed its check digits.
	ErrInvalidCNPJ = errors.New("invalid CNPJ")
	// ErrCompanyUnavailable means the registry is down and no name was given.
	ErrCompanyUnavailable = errors.New("company data unavailable")
)

// RegisterInput is what a new account supplies.
type RegisterInput struct {
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

// Service is what the account endpoints need.
type Service interface {
	Register(ctx context.Context, in RegisterInput) (Session, error)
	Login(ctx context.Context, email, password string) (Session, error)
	ForgotPassword(ctx context.Context, email string)
	ResetPassword(ctx context.Context, token, password string) error
	LookupCNPJ(ctx context.Context, cnpj string) (company.Company, error)
	Me(ctx context.Context, userID string) (User, company.Company, error)
}

type service struct {
	accounts  Accounts
	companies company.Repository
	directory company.Directory
	log       *slog.Logger
}

// NewService returns a Service on top of its stores.
func NewService(accounts Accounts, companies company.Repository, directory company.Directory, log *slog.Logger) Service {
	return &service{accounts: accounts, companies: companies, directory: directory, log: log}
}

func (s *service) Register(ctx context.Context, in RegisterInput) (Session, error) {
	in.Name, in.Email = strings.TrimSpace(in.Name), strings.TrimSpace(in.Email)
	in.RazaoSocial = strings.TrimSpace(in.RazaoSocial)
	cnpj := company.NormalizeCNPJ(in.CNPJ)
	if in.Name == "" || !validEmail(in.Email) || !validPassword(in.Password) || !company.ValidCNPJ(cnpj) {
		return Session{}, ErrInvalidRegistration
	}
	if !validProfile(in.Phone, in.JobFunction, in.BuyingRoles) {
		return Session{}, ErrInvalidProfile
	}
	if in.BuyingRoles == nil {
		in.BuyingRoles = []string{}
	}

	c, err := s.resolveCompany(ctx, cnpj, in.RazaoSocial)
	if err != nil {
		return Session{}, err
	}
	c, err = s.companies.Create(ctx, c)
	if err != nil {
		return Session{}, err
	}

	// Whoever registers a company administers it. A client can't choose this.
	u, err := s.accounts.Create(ctx, User{
		Email:       in.Email,
		Name:        in.Name,
		Permission:  "ADM",
		Active:      true,
		Company:     c.ID,
		Phone:       in.Phone,
		JobFunction: in.JobFunction,
		JobTitle:    strings.TrimSpace(in.JobTitle),
		Seniority:   strings.TrimSpace(in.Seniority),
		BuyingRoles: in.BuyingRoles,
	}, in.Password)
	if err != nil {
		if delErr := s.companies.Delete(ctx, c.ID); delErr != nil {
			s.log.Error("remove company after failed registration", "company", c.ID, "err", delErr)
		}
		return Session{}, err
	}

	session, err := s.accounts.Login(ctx, in.Email, in.Password)
	if err != nil {
		s.log.Error("login after registration", "user", u.ID, "err", err)
	}
	return session, err
}

// resolveCompany prefers the registry. If it is down, registration can still
// proceed with the name the user typed.
func (s *service) resolveCompany(ctx context.Context, cnpj, razaoSocial string) (company.Company, error) {
	c, err := s.directory.Lookup(ctx, cnpj)
	if err == nil || errors.Is(err, company.ErrNotFound) {
		return c, err
	}
	s.log.Warn("cnpj lookup", "err", err)
	if razaoSocial == "" {
		return company.Company{}, ErrCompanyUnavailable
	}
	return company.Company{CNPJ: cnpj, RazaoSocial: razaoSocial, CNAEsSecundarios: []company.CNAE{}}, nil
}

func (s *service) Login(ctx context.Context, email, password string) (Session, error) {
	session, err := s.accounts.Login(ctx, strings.TrimSpace(email), password)
	if err != nil {
		return Session{}, err
	}
	if !session.User.Active {
		return Session{}, ErrInvalidCredentials
	}
	return session, nil
}

// ForgotPassword returns nothing, so its caller can't reveal which emails exist.
func (s *service) ForgotPassword(ctx context.Context, email string) {
	email = strings.TrimSpace(email)
	if !validEmail(email) {
		return
	}
	if err := s.accounts.RequestPasswordReset(ctx, email); err != nil {
		s.log.Error("request password reset", "err", err)
	}
}

func (s *service) ResetPassword(ctx context.Context, token, password string) error {
	if token == "" || !validPassword(password) {
		return ErrInvalidReset
	}
	return s.accounts.ConfirmPasswordReset(ctx, token, password)
}

func (s *service) LookupCNPJ(ctx context.Context, cnpj string) (company.Company, error) {
	cnpj = company.NormalizeCNPJ(cnpj)
	if !company.ValidCNPJ(cnpj) {
		return company.Company{}, ErrInvalidCNPJ
	}
	c, err := s.directory.Lookup(ctx, cnpj)
	if err != nil && !errors.Is(err, company.ErrNotFound) {
		s.log.Warn("cnpj lookup", "err", err)
		return company.Company{}, ErrCompanyUnavailable
	}
	return c, err
}

func (s *service) Me(ctx context.Context, userID string) (User, company.Company, error) {
	u, err := s.accounts.Get(ctx, userID)
	if err != nil {
		return User{}, company.Company{}, err
	}
	c, err := s.companies.Get(ctx, u.Company)
	return u, c, err
}

func validEmail(s string) bool {
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s
}

func validPassword(s string) bool {
	return len(s) >= minPassword && len(s) <= maxPassword
}

package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/you/orderguard/internal/company"
)

const (
	validCNPJ = "45334465000117"
	password  = "correct-horse-battery"
)

type fakeAccounts struct {
	Accounts
	created   []User
	createErr error
	login     Session
	loginErr  error
}

func (f *fakeAccounts) Create(_ context.Context, u User, _ string) (User, error) {
	if f.createErr != nil {
		return User{}, f.createErr
	}
	f.created = append(f.created, u)
	return u, nil
}

func (f *fakeAccounts) Login(context.Context, string, string) (Session, error) {
	return f.login, f.loginErr
}

type fakeCompanies struct {
	company.Repository
	created, deleted []string
	createErr        error
}

func (f *fakeCompanies) Create(_ context.Context, c company.Company) (company.Company, error) {
	if f.createErr != nil {
		return company.Company{}, f.createErr
	}
	f.created = append(f.created, c.RazaoSocial)
	c.ID = "c1"
	return c, nil
}

func (f *fakeCompanies) Delete(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)
	return nil
}

type fakeDirectory struct {
	found company.Company
	err   error
}

func (f fakeDirectory) Lookup(context.Context, string) (company.Company, error) {
	return f.found, f.err
}

func newService(a Accounts, c company.Repository, d company.Directory) Service {
	return NewService(a, c, d, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func input() RegisterInput {
	return RegisterInput{Name: " Ana ", Email: "ana@empresa.com.br", Password: password, CNPJ: "45.334.465/0001-17", RazaoSocial: "Digitada"}
}

func TestRegisterMakesTheFirstUserAnAdmin(t *testing.T) {
	accounts := &fakeAccounts{login: Session{Token: "t"}}
	companies := &fakeCompanies{}
	s, err := newService(accounts, companies, fakeDirectory{found: company.Company{RazaoSocial: "Receita"}}).Register(context.Background(), input())
	if err != nil {
		t.Fatal(err)
	}
	if s.Token != "t" || companies.created[0] != "Receita" {
		t.Fatalf("session = %+v, companies = %v", s, companies.created)
	}
	u := accounts.created[0]
	if u.Permission != "ADM" || !u.Active || u.Company != "c1" || u.Name != "Ana" || u.BuyingRoles == nil {
		t.Fatalf("user = %+v", u)
	}
}

func TestRegisterFallsBackToTheTypedNameWhenTheRegistryIsDown(t *testing.T) {
	companies := &fakeCompanies{}
	down := fakeDirectory{err: errors.New("timeout")}
	if _, err := newService(&fakeAccounts{}, companies, down).Register(context.Background(), input()); err != nil {
		t.Fatal(err)
	}
	if companies.created[0] != "Digitada" {
		t.Fatalf("companies = %v", companies.created)
	}

	in := input()
	in.RazaoSocial = " "
	if _, err := newService(&fakeAccounts{}, companies, down).Register(context.Background(), in); !errors.Is(err, ErrCompanyUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestRegisterRemovesTheCompanyWhenTheUserFails(t *testing.T) {
	companies := &fakeCompanies{}
	accounts := &fakeAccounts{createErr: ErrEmailTaken}
	_, err := newService(accounts, companies, fakeDirectory{}).Register(context.Background(), input())
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("err = %v", err)
	}
	if len(companies.deleted) != 1 {
		t.Fatalf("deleted = %v", companies.deleted)
	}
}

func TestRegisterRejectsBadInputBeforeTouchingAnyStore(t *testing.T) {
	for name, change := range map[string]func(*RegisterInput){
		"no name":        func(in *RegisterInput) { in.Name = " " },
		"bad email":      func(in *RegisterInput) { in.Email = "ana" },
		"short password": func(in *RegisterInput) { in.Password = "short" },
		"bad cnpj":       func(in *RegisterInput) { in.CNPJ = "45334465000118" },
	} {
		t.Run(name, func(t *testing.T) {
			companies := &fakeCompanies{}
			in := input()
			change(&in)
			_, err := newService(&fakeAccounts{}, companies, fakeDirectory{}).Register(context.Background(), in)
			if !errors.Is(err, ErrInvalidRegistration) || len(companies.created) != 0 {
				t.Fatalf("err = %v, companies = %v", err, companies.created)
			}
		})
	}
}

func TestLoginHidesInactiveAccounts(t *testing.T) {
	accounts := &fakeAccounts{login: Session{Token: "t", User: User{Active: false}}}
	_, err := newService(accounts, &fakeCompanies{}, fakeDirectory{}).Login(context.Background(), "ana@empresa.com.br", password)
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v", err)
	}
}

func TestLookupCNPJ(t *testing.T) {
	ctx := context.Background()
	svc := newService(&fakeAccounts{}, &fakeCompanies{}, fakeDirectory{err: errors.New("timeout")})
	if _, err := svc.LookupCNPJ(ctx, "123"); !errors.Is(err, ErrInvalidCNPJ) {
		t.Fatalf("bad cnpj: %v", err)
	}
	if _, err := svc.LookupCNPJ(ctx, validCNPJ); !errors.Is(err, ErrCompanyUnavailable) {
		t.Fatalf("registry down: %v", err)
	}
}

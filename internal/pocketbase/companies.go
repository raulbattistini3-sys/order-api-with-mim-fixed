package pocketbase

import (
	"context"
	"errors"

	"github.com/you/orderguard/internal/company"
)

const companiesCollection = "companies"

// CompanyRepository implements company.Repository.
type CompanyRepository struct {
	pb *Client
}

// NewCompanyRepository returns a CompanyRepository using pb.
func NewCompanyRepository(pb *Client) *CompanyRepository {
	return &CompanyRepository{pb: pb}
}

// Create inserts c.
func (r *CompanyRepository) Create(ctx context.Context, c company.Company) (company.Company, error) {
	var out company.Company
	err := r.pb.Create(ctx, companiesCollection, c, &out)
	if errors.Is(err, ErrNotUnique) {
		return company.Company{}, company.ErrDuplicate
	}
	return out, err
}

// Get returns a company by id.
func (r *CompanyRepository) Get(ctx context.Context, id string) (company.Company, error) {
	var out company.Company
	err := r.pb.Get(ctx, companiesCollection, id, &out)
	if errors.Is(err, ErrNotFound) {
		return company.Company{}, company.ErrNotFound
	}
	return out, err
}

// Delete removes a company by id.
func (r *CompanyRepository) Delete(ctx context.Context, id string) error {
	return r.pb.Delete(ctx, companiesCollection, id)
}

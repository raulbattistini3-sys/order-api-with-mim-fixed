package order

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Page sizes for a customer's orders.
const (
	defaultPage = 100
	maxPage     = 200
	// defaultMatches and maxMatches size a search's results.
	defaultMatches = 20
	maxMatches     = 100
)

var (
	// ErrInvalidLimit means a page size is out of range.
	ErrInvalidLimit = fmt.Errorf("limit must be from 1 to %d", maxPage)
	// ErrInvalidCursor means a cursor wasn't one this service issued.
	ErrInvalidCursor = errors.New("invalid cursor")
	// ErrInvalidSearch means a search has no text or an out-of-range limit.
	ErrInvalidSearch = fmt.Errorf("q (1-200) is required, and limit must be from 1 to %d", maxMatches)
	// ErrSearchDisabled means no search engine is configured.
	ErrSearchDisabled = errors.New("search is not configured")
)

// Searcher finds a customer's orders by text.
type Searcher interface {
	Search(ctx context.Context, customer, q string, limit int) ([]Order, error)
}

// NoSearch is the Searcher to use when no search engine is configured.
type NoSearch struct{}

// Search always returns ErrSearchDisabled.
func (NoSearch) Search(context.Context, string, string, int) ([]Order, error) {
	return nil, ErrSearchDisabled
}

// Page is one page of a customer's orders, newest first. NextCursor is empty on
// the last page.
type Page struct {
	Orders     []Order `json:"orders"`
	NextCursor string  `json:"next_cursor"`
}

// Service is what the order endpoints need.
type Service interface {
	Place(ctx context.Context, o Order) (Order, error)
	Recent(ctx context.Context, customer string) ([]Order, int, error)
	Page(ctx context.Context, customer string, limit int, cursor string) (Page, error)
	Totals(ctx context.Context, customer string) (Totals, error)
	Search(ctx context.Context, customer, q string, limit int) ([]Order, error)
}

type service struct {
	repo     Repository
	searcher Searcher
}

// NewService returns a Service backed by repo and searcher.
func NewService(repo Repository, searcher Searcher) Service {
	return &service{repo: repo, searcher: searcher}
}

func (s *service) Search(ctx context.Context, customer, q string, limit int) ([]Order, error) {
	q = strings.TrimSpace(q)
	if q == "" || len(q) > 200 || limit < 1 || limit > maxMatches {
		return nil, ErrInvalidSearch
	}
	orders, err := s.searcher.Search(ctx, customer, q, limit)
	if orders == nil {
		orders = []Order{}
	}
	return orders, err
}

func (s *service) Place(ctx context.Context, o Order) (Order, error) {
	if err := o.Validate(); err != nil {
		return Order{}, err
	}
	return s.repo.Create(ctx, o)
}

// Recent returns the 50 newest orders and the customer's total.
func (s *service) Recent(ctx context.Context, customer string) ([]Order, int, error) {
	orders, total, err := s.repo.ListByCustomer(ctx, customer, 50)
	if orders == nil {
		orders = []Order{}
	}
	return orders, total, err
}

func (s *service) Page(ctx context.Context, customer string, limit int, cursor string) (Page, error) {
	if limit < 1 || limit > maxPage {
		return Page{}, ErrInvalidLimit
	}
	after, err := decodeCursor(cursor)
	if err != nil {
		return Page{}, ErrInvalidCursor
	}

	// One extra row says whether another page exists, without counting them all.
	orders, err := s.repo.ListAfter(ctx, customer, after, limit+1)
	if err != nil {
		return Page{}, err
	}
	next := ""
	if len(orders) > limit {
		orders = orders[:limit]
		last := orders[limit-1]
		next = encodeCursor(Cursor{Created: last.Created, ID: last.ID})
	}
	if orders == nil {
		orders = []Order{}
	}
	return Page{Orders: orders, NextCursor: next}, nil
}

func (s *service) Totals(ctx context.Context, customer string) (Totals, error) {
	return s.repo.Totals(ctx, customer)
}

// Cursors are opaque to clients, so their format can change.
func encodeCursor(c Cursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.Created + "|" + c.ID))
}

func decodeCursor(s string) (*Cursor, error) {
	if s == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	created, id, ok := strings.Cut(string(raw), "|")
	if !ok || created == "" || id == "" {
		return nil, errors.New("order: malformed cursor")
	}
	return &Cursor{Created: created, ID: id}, nil
}

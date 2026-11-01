// Package order is the domain: what an order is, and the HTTP API over it.
package order

import (
	"context"
	"errors"
)

// RefHeader carries the verified idempotency key from the gateway to the API.
// Requests that reach the API any other way have no ref and get no protection.
const RefHeader = "X-Order-Ref"

var (
	// ErrDuplicate means an order with this idempotency key already exists.
	ErrDuplicate = errors.New("order: duplicate idempotency key")
	// ErrInvalid means the order failed validation.
	ErrInvalid = errors.New("customer (1-100), item (1-200) and quantity (1-10000) are required")
)

// Order is one order record.
type Order struct {
	ID             string `json:"id,omitempty"`
	Customer       string `json:"customer"`
	Item           string `json:"item"`
	Quantity       int    `json:"quantity"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	Company        string `json:"company,omitempty"`
	Created        string `json:"created,omitempty"`
}

// Validate checks the fields a client controls.
func (o Order) Validate() error {
	if o.Customer == "" || len(o.Customer) > 100 || o.Item == "" || len(o.Item) > 200 || o.Quantity < 1 || o.Quantity > 10_000 {
		return ErrInvalid
	}
	return nil
}

// Repository stores orders. Create returns ErrDuplicate when IdempotencyKey is
// set and already taken.
type Repository interface {
	Create(ctx context.Context, o Order) (Order, error)
	ListByCustomer(ctx context.Context, customer string, limit int) ([]Order, int, error)
}

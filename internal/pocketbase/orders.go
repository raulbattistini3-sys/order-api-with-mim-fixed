package pocketbase

import (
	"context"
	"errors"

	"github.com/you/orderguard/internal/order"
)

const ordersCollection = "orders"

// OrderRepository implements order.Repository.
type OrderRepository struct {
	pb *Client
}

// NewOrderRepository returns an OrderRepository using pb.
func NewOrderRepository(pb *Client) *OrderRepository {
	return &OrderRepository{pb: pb}
}

// Create inserts o. Orders carrying an idempotency key are unique on it.
func (r *OrderRepository) Create(ctx context.Context, o order.Order) (order.Order, error) {
	var out order.Order
	err := r.pb.Create(ctx, ordersCollection, o, &out)
	if errors.Is(err, ErrNotUnique) {
		return order.Order{}, order.ErrDuplicate
	}
	return out, err
}

// ListByCustomer returns up to limit newest orders and the total count.
func (r *OrderRepository) ListByCustomer(ctx context.Context, customer string, limit int) ([]order.Order, int, error) {
	page, err := List[order.Order](ctx, r.pb, ordersCollection, "customer="+Quote(customer), "-created", limit)
	return page.Items, page.TotalItems, err
}

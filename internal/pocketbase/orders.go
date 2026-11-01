package pocketbase

import (
	"context"
	"errors"

	"github.com/you/orderguard/internal/order"
)

const (
	ordersCollection    = "orders"
	orderTotalsView     = "order_totals"
	orderItemTotalsView = "order_item_totals"
	topItems            = 5
)

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

// ListAfter returns up to limit orders older than after, newest first.
func (r *OrderRepository) ListAfter(ctx context.Context, customer string, after *order.Cursor, limit int) ([]order.Order, error) {
	filter := "customer=" + Quote(customer)
	if after != nil {
		created, id := Quote(after.Created), Quote(after.ID)
		filter += " && (created<" + created + " || (created=" + created + " && id<" + id + "))"
	}
	return Scroll[order.Order](ctx, r.pb, ordersCollection, filter, "-created,-id", limit)
}

// Totals reads the aggregate views. A customer with no orders has no rows there.
func (r *OrderRepository) Totals(ctx context.Context, customer string) (order.Totals, error) {
	var row struct {
		Orders int `json:"order_count"`
		Units  int `json:"units"`
		Items  int `json:"items"`
	}
	err := r.pb.Get(ctx, orderTotalsView, customer, &row)
	if errors.Is(err, ErrNotFound) {
		return order.Totals{TopItems: []order.ItemTotal{}}, nil
	}
	if err != nil {
		return order.Totals{}, err
	}
	type itemRow struct {
		Item   string `json:"item"`
		Units  int    `json:"units"`
		Orders int    `json:"order_count"`
	}
	items, err := Scroll[itemRow](ctx, r.pb, orderItemTotalsView, "customer="+Quote(customer), "-units", topItems)
	if err != nil {
		return order.Totals{}, err
	}
	totals := order.Totals{Orders: row.Orders, Units: row.Units, Items: row.Items, TopItems: make([]order.ItemTotal, len(items))}
	for i, it := range items {
		totals.TopItems[i] = order.ItemTotal(it)
	}
	return totals, nil
}

package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/you/orderguard/internal/auth"
)

// memRepo holds orders newest first, the way the store returns them.
type memRepo struct {
	orders []Order
	totals Totals
}

func (r *memRepo) Create(_ context.Context, o Order) (Order, error) { return o, nil }

func (r *memRepo) ListByCustomer(context.Context, string, int) ([]Order, int, error) {
	return nil, 0, nil
}

func (r *memRepo) ListAfter(_ context.Context, customer string, after *Cursor, limit int) ([]Order, error) {
	var out []Order
	passed := after == nil
	for _, o := range r.orders {
		if o.Customer != customer {
			continue
		}
		if passed && len(out) < limit {
			out = append(out, o)
		}
		passed = passed || (o.Created == after.Created && o.ID == after.ID)
	}
	return out, nil
}

func (r *memRepo) Totals(_ context.Context, customer string) (Totals, error) {
	if customer != "u1" {
		return Totals{}, nil
	}
	return r.totals, nil
}

func newMux(repo Repository) *http.ServeMux {
	mux := http.NewServeMux()
	Routes(mux, NewService(repo, stubSearch{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	return mux
}

func get(mux *http.ServeMux, target, user string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set(auth.UserHeader, user)
	req.Header.Set(auth.CompanyHeader, "c1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

type pageBody struct {
	Orders     []Order `json:"orders"`
	NextCursor string  `json:"next_cursor"`
}

func TestPagesWalkEveryOrderOnce(t *testing.T) {
	repo := &memRepo{}
	for i := range 25 {
		// Every order shares a timestamp, as a bulk import's can.
		repo.orders = append(repo.orders, Order{ID: fmt.Sprintf("o%02d", 24-i), Customer: "u1", Created: "2026-11-01 10:00:00.000Z"})
	}
	repo.orders = append(repo.orders, Order{ID: "other", Customer: "u2", Created: "2026-11-01 10:00:00.000Z"})
	mux := newMux(repo)

	var seen []string
	pages, target := 0, "/me/orders?limit=10"
	for {
		rec := get(mux, target, "u1")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
		}
		var page pageBody
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		pages++
		for _, o := range page.Orders {
			seen = append(seen, o.ID)
		}
		if page.NextCursor == "" {
			break
		}
		target = "/me/orders?limit=10&cursor=" + url.QueryEscape(page.NextCursor)
	}
	if pages != 3 || len(seen) != 25 {
		t.Fatalf("pages = %d, seen = %d", pages, len(seen))
	}
	if slices.Contains(seen, "other") {
		t.Fatal("listed another customer's order")
	}
	sorted := slices.Clone(seen)
	slices.Sort(sorted)
	if len(slices.Compact(sorted)) != 25 {
		t.Fatalf("duplicates in %v", seen)
	}
}

func TestLastFullPageHasNoCursor(t *testing.T) {
	repo := &memRepo{orders: []Order{{ID: "a", Customer: "u1", Created: "t"}, {ID: "b", Customer: "u1", Created: "t"}}}
	var page pageBody
	json.Unmarshal(get(newMux(repo), "/me/orders?limit=2", "u1").Body.Bytes(), &page)
	if len(page.Orders) != 2 || page.NextCursor != "" {
		t.Fatalf("page = %+v", page)
	}
}

func TestEmptyListIsAnArray(t *testing.T) {
	rec := get(newMux(&memRepo{}), "/me/orders", "u1")
	if !strings.Contains(rec.Body.String(), `"orders":[]`) {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestRejectsBadPageParams(t *testing.T) {
	mux := newMux(&memRepo{})
	for _, target := range []string{
		"/me/orders?limit=0",
		"/me/orders?limit=201",
		"/me/orders?limit=x",
		"/me/orders?cursor=!!!",
		"/me/orders?cursor=" + encodeCursor(Cursor{}),
	} {
		if rec := get(mux, target, "u1"); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d", target, rec.Code)
		}
	}
}

// stubSearch records nothing and answers with one order naming its customer.
type stubSearch struct{ err error }

func (s stubSearch) Search(_ context.Context, customer, q string, _ int) ([]Order, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []Order{{ID: "hit", Customer: customer, Item: q}}, nil
}

func TestSearch(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tt := range []struct {
		name     string
		searcher Searcher
		target   string
		want     int
	}{
		{"finds", stubSearch{}, "/me/orders/search?q=teclado", http.StatusOK},
		{"no text", stubSearch{}, "/me/orders/search?q=%20", http.StatusBadRequest},
		{"limit too high", stubSearch{}, "/me/orders/search?q=a&limit=101", http.StatusBadRequest},
		{"bad limit", stubSearch{}, "/me/orders/search?q=a&limit=x", http.StatusBadRequest},
		{"not configured", NoSearch{}, "/me/orders/search?q=a", http.StatusServiceUnavailable},
		{"engine down", stubSearch{err: errors.New("down")}, "/me/orders/search?q=a", http.StatusBadGateway},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			Routes(mux, NewService(&memRepo{}, tt.searcher), log)
			if rec := get(mux, tt.target, "u1"); rec.Code != tt.want {
				t.Fatalf("status = %d, want %d, body %s", rec.Code, tt.want, rec.Body)
			}
		})
	}
}

func TestSearchUsesTheSessionsCustomer(t *testing.T) {
	rec := get(newMux(&memRepo{}), "/me/orders/search?q=a&customer=someone-else", "u1")
	if !strings.Contains(rec.Body.String(), `"customer":"u1"`) {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestSummaryIsScopedToTheSession(t *testing.T) {
	repo := &memRepo{totals: Totals{Orders: 7, Units: 9, Items: 2}}
	rec := get(newMux(repo), "/me/orders/summary", "u1")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"orders":7`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := get(newMux(repo), "/me/orders/summary", "u2"); strings.Contains(rec.Body.String(), `"orders":7`) {
		t.Fatalf("leaked totals: %s", rec.Body)
	}
}

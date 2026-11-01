package order

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/you/orderguard/internal/auth"
	"github.com/you/orderguard/internal/httpjson"
)

// Page sizes for GET /me/orders.
const (
	defaultPage = 100
	maxPage     = 200
)

// Handler serves the order endpoints.
type Handler struct {
	repo Repository
	log  *slog.Logger
}

// NewHandler returns a Handler backed by repo.
func NewHandler(repo Repository, log *slog.Logger) *Handler {
	return &Handler{repo: repo, log: log}
}

// Register adds the order routes to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /orders", h.create)
	mux.HandleFunc("GET /orders", h.list)
	mux.Handle("POST /me/orders", auth.RequireIdentity(http.HandlerFunc(h.createMine)))
	mux.Handle("GET /me/orders", auth.RequireIdentity(http.HandlerFunc(h.listMine)))
	mux.Handle("GET /me/orders/summary", auth.RequireIdentity(http.HandlerFunc(h.summaryMine)))
}

type createRequest struct {
	Customer string `json:"customer"`
	Item     string `json:"item"`
	Quantity int    `json:"quantity"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if !decode(w, r, &req) {
		return
	}
	h.place(w, r, Order{
		Customer:       strings.TrimSpace(req.Customer),
		Item:           strings.TrimSpace(req.Item),
		Quantity:       req.Quantity,
		IdempotencyKey: r.Header.Get(RefHeader),
	})
}

// createMine takes the customer from the session, never from the body, so a
// user can't order in someone else's name.
func (h *Handler) createMine(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Item     string `json:"item"`
		Quantity int    `json:"quantity"`
	}
	if !decode(w, r, &req) {
		return
	}
	id, _ := auth.IdentityFrom(r.Context())
	h.place(w, r, Order{
		Customer:       id.UserID,
		Company:        id.CompanyID,
		Item:           strings.TrimSpace(req.Item),
		Quantity:       req.Quantity,
		IdempotencyKey: r.Header.Get(RefHeader),
	})
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

func (h *Handler) place(w http.ResponseWriter, r *http.Request, in Order) {
	if err := in.Validate(); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	o, err := h.repo.Create(r.Context(), in)
	switch {
	case errors.Is(err, ErrDuplicate):
		httpjson.Error(w, http.StatusConflict, "order for this Idempotency-Key already exists")
	case err != nil:
		h.log.Error("create order", "err", err)
		httpjson.Error(w, http.StatusInternalServerError, "internal error")
	default:
		httpjson.Write(w, http.StatusCreated, o)
	}
}

// list handles GET /orders?customer=X and returns the count and 50 newest orders.
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	customer := r.URL.Query().Get("customer")
	if customer == "" {
		httpjson.Error(w, http.StatusBadRequest, "customer query parameter required")
		return
	}
	h.respondList(w, r, customer)
}

// listMine handles GET /me/orders?limit=N&cursor=C. It returns one page, newest
// first, and next_cursor when there are more.
func (h *Handler) listMine(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.IdentityFrom(r.Context())
	limit := defaultPage
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxPage {
			httpjson.Error(w, http.StatusBadRequest, "limit must be from 1 to 200")
			return
		}
		limit = n
	}
	after, err := decodeCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		httpjson.Error(w, http.StatusBadRequest, "invalid cursor")
		return
	}

	// One extra row says whether another page exists, without counting them all.
	orders, err := h.repo.ListAfter(r.Context(), id.UserID, after, limit+1)
	if err != nil {
		h.log.Error("list orders", "err", err)
		httpjson.Error(w, http.StatusInternalServerError, "internal error")
		return
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
	httpjson.Write(w, http.StatusOK, map[string]any{"orders": orders, "next_cursor": next})
}

func (h *Handler) summaryMine(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.IdentityFrom(r.Context())
	totals, err := h.repo.Totals(r.Context(), id.UserID)
	if err != nil {
		h.log.Error("order totals", "err", err)
		httpjson.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpjson.Write(w, http.StatusOK, totals)
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

func (h *Handler) respondList(w http.ResponseWriter, r *http.Request, customer string) {
	orders, total, err := h.repo.ListByCustomer(r.Context(), customer, 50)
	if err != nil {
		h.log.Error("list orders", "err", err)
		httpjson.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if orders == nil {
		orders = []Order{}
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"count": total, "orders": orders})
}

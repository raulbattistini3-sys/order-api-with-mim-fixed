package order

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/you/orderguard/internal/auth"
	"github.com/you/orderguard/internal/httpjson"
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

func (h *Handler) listMine(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.IdentityFrom(r.Context())
	h.respondList(w, r, id.UserID)
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

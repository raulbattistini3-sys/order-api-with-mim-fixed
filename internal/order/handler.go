package order

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

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
}

type createRequest struct {
	Customer string `json:"customer"`
	Item     string `json:"item"`
	Quantity int    `json:"quantity"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		httpjson.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	in := Order{
		Customer:       strings.TrimSpace(req.Customer),
		Item:           strings.TrimSpace(req.Item),
		Quantity:       req.Quantity,
		IdempotencyKey: r.Header.Get(RefHeader),
	}
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

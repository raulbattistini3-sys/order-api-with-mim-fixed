package order

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/you/orderguard/internal/auth"
	"github.com/you/orderguard/internal/httpjson"
)

// Routes adds the order endpoints to mux.
func Routes(mux *http.ServeMux, svc Service, log *slog.Logger) {
	mux.Handle("POST /orders", create(svc, log))
	mux.Handle("GET /orders", list(svc, log))
	mux.Handle("POST /me/orders", auth.RequireIdentity(createMine(svc, log)))
	mux.Handle("GET /me/orders", auth.RequireIdentity(listMine(svc, log)))
	mux.Handle("GET /me/orders/summary", auth.RequireIdentity(summaryMine(svc, log)))
}

func create(svc Service, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Customer string `json:"customer"`
			Item     string `json:"item"`
			Quantity int    `json:"quantity"`
		}
		if !decode(w, r, &req) {
			return
		}
		place(w, r, svc, log, Order{
			Customer:       strings.TrimSpace(req.Customer),
			Item:           strings.TrimSpace(req.Item),
			Quantity:       req.Quantity,
			IdempotencyKey: r.Header.Get(RefHeader),
		})
	}
}

// createMine takes the customer from the session, never from the body, so a
// user can't order in someone else's name.
func createMine(svc Service, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Item     string `json:"item"`
			Quantity int    `json:"quantity"`
		}
		if !decode(w, r, &req) {
			return
		}
		id, _ := auth.IdentityFrom(r.Context())
		place(w, r, svc, log, Order{
			Customer:       id.UserID,
			Company:        id.CompanyID,
			Item:           strings.TrimSpace(req.Item),
			Quantity:       req.Quantity,
			IdempotencyKey: r.Header.Get(RefHeader),
		})
	}
}

func place(w http.ResponseWriter, r *http.Request, svc Service, log *slog.Logger, in Order) {
	o, err := svc.Place(r.Context(), in)
	switch {
	case errors.Is(err, ErrInvalid):
		httpjson.Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrDuplicate):
		httpjson.Error(w, http.StatusConflict, "order for this Idempotency-Key already exists")
	case err != nil:
		log.Error("create order", "err", err)
		httpjson.Error(w, http.StatusInternalServerError, "internal error")
	default:
		httpjson.Write(w, http.StatusCreated, o)
	}
}

// list handles GET /orders?customer=X and returns the count and 50 newest orders.
func list(svc Service, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customer := r.URL.Query().Get("customer")
		if customer == "" {
			httpjson.Error(w, http.StatusBadRequest, "customer query parameter required")
			return
		}
		orders, total, err := svc.Recent(r.Context(), customer)
		if err != nil {
			log.Error("list orders", "err", err)
			httpjson.Error(w, http.StatusInternalServerError, "internal error")
			return
		}
		httpjson.Write(w, http.StatusOK, map[string]any{"count": total, "orders": orders})
	}
}

// listMine handles GET /me/orders?limit=N&cursor=C.
func listMine(svc Service, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, _ := auth.IdentityFrom(r.Context())
		limit := defaultPage
		if v := r.URL.Query().Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				httpjson.Error(w, http.StatusBadRequest, ErrInvalidLimit.Error())
				return
			}
			limit = n
		}
		page, err := svc.Page(r.Context(), id.UserID, limit, r.URL.Query().Get("cursor"))
		switch {
		case errors.Is(err, ErrInvalidLimit), errors.Is(err, ErrInvalidCursor):
			httpjson.Error(w, http.StatusBadRequest, err.Error())
		case err != nil:
			log.Error("list orders", "err", err)
			httpjson.Error(w, http.StatusInternalServerError, "internal error")
		default:
			httpjson.Write(w, http.StatusOK, page)
		}
	}
}

func summaryMine(svc Service, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, _ := auth.IdentityFrom(r.Context())
		totals, err := svc.Totals(r.Context(), id.UserID)
		if err != nil {
			log.Error("order totals", "err", err)
			httpjson.Error(w, http.StatusInternalServerError, "internal error")
			return
		}
		httpjson.Write(w, http.StatusOK, totals)
	}
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

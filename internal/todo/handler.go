package todo

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/you/orderguard/internal/auth"
	"github.com/you/orderguard/internal/httpjson"
)

// Routes adds the to-do endpoints to mux.
func Routes(mux *http.ServeMux, svc Service, log *slog.Logger) {
	mux.Handle("GET /me/todos", auth.RequireIdentity(list(svc, log)))
	mux.Handle("POST /me/todos", auth.RequireIdentity(create(svc, log)))
	mux.Handle("PATCH /me/todos/{id}", auth.RequireIdentity(update(svc, log)))
	mux.Handle("DELETE /me/todos/{id}", auth.RequireIdentity(remove(svc, log)))
}

func user(r *http.Request) string {
	id, _ := auth.IdentityFrom(r.Context())
	return id.UserID
}

func list(svc Service, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		todos, err := svc.List(r.Context(), user(r))
		if err != nil {
			fail(w, log, "list todos", err)
			return
		}
		httpjson.Write(w, http.StatusOK, todos)
	}
}

func create(svc Service, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Title string `json:"title"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
			httpjson.Error(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		t, err := svc.Add(r.Context(), user(r), req.Title)
		if err != nil {
			fail(w, log, "create todo", err)
			return
		}
		httpjson.Write(w, http.StatusCreated, t)
	}
}

func update(svc Service, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Done bool `json:"done"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
			httpjson.Error(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		t, err := svc.SetDone(r.Context(), user(r), r.PathValue("id"), req.Done)
		if err != nil {
			fail(w, log, "update todo", err)
			return
		}
		httpjson.Write(w, http.StatusOK, t)
	}
}

func remove(svc Service, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := svc.Remove(r.Context(), user(r), r.PathValue("id")); err != nil {
			fail(w, log, "delete todo", err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func fail(w http.ResponseWriter, log *slog.Logger, what string, err error) {
	switch {
	case errors.Is(err, ErrInvalidTitle):
		httpjson.Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotFound):
		httpjson.Error(w, http.StatusNotFound, "to-do not found")
	default:
		log.Error(what, "err", err)
		httpjson.Error(w, http.StatusInternalServerError, "internal error")
	}
}

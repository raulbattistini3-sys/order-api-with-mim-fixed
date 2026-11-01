// Package todo is a per-user to-do list.
package todo

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/you/orderguard/internal/auth"
	"github.com/you/orderguard/internal/httpjson"
)

// ErrNotFound means the to-do doesn't exist or belongs to someone else.
var ErrNotFound = errors.New("todo: not found")

// Todo is one to-do item.
type Todo struct {
	ID      string `json:"id,omitempty"`
	User    string `json:"user,omitempty"`
	Title   string `json:"title"`
	Done    bool   `json:"done"`
	Created string `json:"created,omitempty"`
}

// Repository stores to-dos. Every method is scoped to one user.
type Repository interface {
	List(ctx context.Context, user string) ([]Todo, error)
	Create(ctx context.Context, t Todo) (Todo, error)
	SetDone(ctx context.Context, user, id string, done bool) (Todo, error)
	Delete(ctx context.Context, user, id string) error
}

// Handler serves the to-do endpoints.
type Handler struct {
	repo Repository
	log  *slog.Logger
}

// NewHandler returns a Handler backed by repo.
func NewHandler(repo Repository, log *slog.Logger) *Handler {
	return &Handler{repo: repo, log: log}
}

// Register adds the to-do routes to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /me/todos", auth.RequireIdentity(http.HandlerFunc(h.list)))
	mux.Handle("POST /me/todos", auth.RequireIdentity(http.HandlerFunc(h.create)))
	mux.Handle("PATCH /me/todos/{id}", auth.RequireIdentity(http.HandlerFunc(h.update)))
	mux.Handle("DELETE /me/todos/{id}", auth.RequireIdentity(http.HandlerFunc(h.delete)))
}

func user(r *http.Request) string {
	id, _ := auth.IdentityFrom(r.Context())
	return id.UserID
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	todos, err := h.repo.List(r.Context(), user(r))
	if err != nil {
		h.fail(w, "list todos", err)
		return
	}
	if todos == nil {
		todos = []Todo{}
	}
	httpjson.Write(w, http.StatusOK, todos)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		httpjson.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" || len(title) > 200 {
		httpjson.Error(w, http.StatusBadRequest, "title (1-200) is required")
		return
	}
	t, err := h.repo.Create(r.Context(), Todo{User: user(r), Title: title})
	if err != nil {
		h.fail(w, "create todo", err)
		return
	}
	httpjson.Write(w, http.StatusCreated, t)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Done bool `json:"done"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		httpjson.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	t, err := h.repo.SetDone(r.Context(), user(r), r.PathValue("id"), req.Done)
	if err != nil {
		h.fail(w, "update todo", err)
		return
	}
	httpjson.Write(w, http.StatusOK, t)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.repo.Delete(r.Context(), user(r), r.PathValue("id")); err != nil {
		h.fail(w, "delete todo", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) fail(w http.ResponseWriter, what string, err error) {
	if errors.Is(err, ErrNotFound) {
		httpjson.Error(w, http.StatusNotFound, "to-do not found")
		return
	}
	h.log.Error(what, "err", err)
	httpjson.Error(w, http.StatusInternalServerError, "internal error")
}

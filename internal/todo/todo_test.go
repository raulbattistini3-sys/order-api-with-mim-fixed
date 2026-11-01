package todo

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/you/orderguard/internal/auth"
)

type memRepo struct {
	todos []Todo
}

func (r *memRepo) List(_ context.Context, user string) ([]Todo, error) {
	var out []Todo
	for _, t := range r.todos {
		if t.User == user {
			out = append(out, t)
		}
	}
	return out, nil
}

func (r *memRepo) Create(_ context.Context, t Todo) (Todo, error) {
	t.ID = "t1"
	r.todos = append(r.todos, t)
	return t, nil
}

func (r *memRepo) SetDone(_ context.Context, user, id string, done bool) (Todo, error) {
	for i, t := range r.todos {
		if t.ID == id && t.User == user {
			r.todos[i].Done = done
			return r.todos[i], nil
		}
	}
	return Todo{}, ErrNotFound
}

func (r *memRepo) Delete(_ context.Context, user, id string) error {
	if _, err := r.SetDone(context.Background(), user, id, false); err != nil {
		return err
	}
	r.todos = nil
	return nil
}

func TestAddValidatesTitle(t *testing.T) {
	svc := NewService(&memRepo{})
	for _, title := range []string{"", "   ", strings.Repeat("x", 201)} {
		if _, err := svc.Add(context.Background(), "u", title); !errors.Is(err, ErrInvalidTitle) {
			t.Errorf("%q: err = %v", title, err)
		}
	}
	got, err := svc.Add(context.Background(), "u", " comprar ")
	if err != nil || got.Title != "comprar" || got.User != "u" {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

func TestListIsNeverNil(t *testing.T) {
	todos, err := NewService(&memRepo{}).List(context.Background(), "u")
	if err != nil || todos == nil {
		t.Fatalf("todos = %v, err = %v", todos, err)
	}
}

func request(mux *http.ServeMux, method, target, user, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if user != "" {
		req.Header.Set(auth.UserHeader, user)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestRoutes(t *testing.T) {
	mux := http.NewServeMux()
	Routes(mux, NewService(&memRepo{}), slog.New(slog.NewTextHandler(io.Discard, nil)))

	if rec := request(mux, http.MethodGet, "/me/todos", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no identity: %d", rec.Code)
	}
	if rec := request(mux, http.MethodPost, "/me/todos", "u", `{"title":""}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty title: %d", rec.Code)
	}
	if rec := request(mux, http.MethodPost, "/me/todos", "u", `{"title":"a"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d", rec.Code)
	}
	if rec := request(mux, http.MethodPatch, "/me/todos/t1", "someone-else", `{"done":true}`); rec.Code != http.StatusNotFound {
		t.Fatalf("another user's to-do: %d", rec.Code)
	}
	if rec := request(mux, http.MethodPatch, "/me/todos/t1", "u", `{"done":true}`); rec.Code != http.StatusOK {
		t.Fatalf("update: %d", rec.Code)
	}
	if rec := request(mux, http.MethodDelete, "/me/todos/t1", "u", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
}

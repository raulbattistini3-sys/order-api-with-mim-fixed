// Package todo is a per-user to-do list.
package todo

import (
	"context"
	"errors"
	"strings"
)

var (
	// ErrNotFound means the to-do doesn't exist or belongs to someone else.
	ErrNotFound = errors.New("todo: not found")
	// ErrInvalidTitle means a title is empty or too long.
	ErrInvalidTitle = errors.New("title (1-200) is required")
)

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

// Service is what the to-do endpoints need.
type Service interface {
	List(ctx context.Context, user string) ([]Todo, error)
	Add(ctx context.Context, user, title string) (Todo, error)
	SetDone(ctx context.Context, user, id string, done bool) (Todo, error)
	Remove(ctx context.Context, user, id string) error
}

type service struct {
	repo Repository
}

// NewService returns a Service backed by repo.
func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) List(ctx context.Context, user string) ([]Todo, error) {
	todos, err := s.repo.List(ctx, user)
	if todos == nil {
		todos = []Todo{}
	}
	return todos, err
}

func (s *service) Add(ctx context.Context, user, title string) (Todo, error) {
	title = strings.TrimSpace(title)
	if title == "" || len(title) > 200 {
		return Todo{}, ErrInvalidTitle
	}
	return s.repo.Create(ctx, Todo{User: user, Title: title})
}

func (s *service) SetDone(ctx context.Context, user, id string, done bool) (Todo, error) {
	return s.repo.SetDone(ctx, user, id, done)
}

func (s *service) Remove(ctx context.Context, user, id string) error {
	return s.repo.Delete(ctx, user, id)
}

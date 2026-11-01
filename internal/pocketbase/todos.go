package pocketbase

import (
	"context"
	"errors"

	"github.com/you/orderguard/internal/todo"
)

const todosCollection = "todos"

// TodoRepository implements todo.Repository.
type TodoRepository struct {
	pb *Client
}

// NewTodoRepository returns a TodoRepository using pb.
func NewTodoRepository(pb *Client) *TodoRepository {
	return &TodoRepository{pb: pb}
}

// List returns up to 200 of the user's to-dos, newest first.
func (r *TodoRepository) List(ctx context.Context, user string) ([]todo.Todo, error) {
	page, err := List[todo.Todo](ctx, r.pb, todosCollection, "user="+Quote(user), "-created", 200)
	return page.Items, err
}

// Create inserts t.
func (r *TodoRepository) Create(ctx context.Context, t todo.Todo) (todo.Todo, error) {
	var out todo.Todo
	err := r.pb.Create(ctx, todosCollection, t, &out)
	return out, err
}

// SetDone updates one of the user's to-dos.
func (r *TodoRepository) SetDone(ctx context.Context, user, id string, done bool) (todo.Todo, error) {
	if err := r.owned(ctx, user, id); err != nil {
		return todo.Todo{}, err
	}
	var out todo.Todo
	err := r.pb.Update(ctx, todosCollection, id, map[string]any{"done": done}, &out)
	return out, err
}

// Delete removes one of the user's to-dos.
func (r *TodoRepository) Delete(ctx context.Context, user, id string) error {
	if err := r.owned(ctx, user, id); err != nil {
		return err
	}
	return r.pb.Delete(ctx, todosCollection, id)
}

func (r *TodoRepository) owned(ctx context.Context, user, id string) error {
	_, err := First[todo.Todo](ctx, r.pb, todosCollection, "id="+Quote(id)+" && user="+Quote(user))
	if errors.Is(err, ErrNotFound) {
		return todo.ErrNotFound
	}
	return err
}

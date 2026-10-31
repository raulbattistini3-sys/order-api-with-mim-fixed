package pocketbase

import (
	"context"
	"errors"
	"time"

	"github.com/you/orderguard/internal/idempotency"
)

const keysCollection = "idempotency_keys"

// PocketBase's datetime format.
const pbTime = "2006-01-02 15:04:05.000Z"

// IdempotencyStore implements idempotency.Store. The unique index on "key" in
// the idempotency_keys collection is what makes Insert atomic.
type IdempotencyStore struct {
	pb *Client
}

// NewIdempotencyStore returns an IdempotencyStore using pb.
func NewIdempotencyStore(pb *Client) *IdempotencyStore {
	return &IdempotencyStore{pb: pb}
}

type keyRecord struct {
	ID             string `json:"id,omitempty"`
	Key            string `json:"key"`
	Fingerprint    string `json:"fingerprint"`
	Status         string `json:"status"`
	ResponseStatus int    `json:"response_status,omitempty"`
	ResponseBody   string `json:"response_body,omitempty"`
	Created        string `json:"created,omitempty"`
}

func (s *IdempotencyStore) Insert(ctx context.Context, key, fingerprint string) (string, error) {
	var out keyRecord
	err := s.pb.Create(ctx, keysCollection, keyRecord{Key: key, Fingerprint: fingerprint, Status: idempotency.StatusPending}, &out)
	if errors.Is(err, ErrNotUnique) {
		return "", idempotency.ErrExists
	}
	return out.ID, err
}

func (s *IdempotencyStore) Get(ctx context.Context, key string) (idempotency.Record, error) {
	r, err := First[keyRecord](ctx, s.pb, keysCollection, "key="+Quote(key))
	if errors.Is(err, ErrNotFound) {
		return idempotency.Record{}, idempotency.ErrNotFound
	}
	if err != nil {
		return idempotency.Record{}, err
	}
	created, _ := time.Parse(pbTime, r.Created)
	return idempotency.Record{
		ID:             r.ID,
		Key:            r.Key,
		Fingerprint:    r.Fingerprint,
		Status:         r.Status,
		ResponseStatus: r.ResponseStatus,
		ResponseBody:   []byte(r.ResponseBody),
		Created:        created,
	}, nil
}

func (s *IdempotencyStore) Complete(ctx context.Context, id string, status int, body []byte) error {
	patch := map[string]any{"status": idempotency.StatusCompleted, "response_status": status, "response_body": string(body)}
	return s.pb.Update(ctx, keysCollection, id, patch, nil)
}

func (s *IdempotencyStore) Delete(ctx context.Context, id string) error {
	err := s.pb.Delete(ctx, keysCollection, id)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

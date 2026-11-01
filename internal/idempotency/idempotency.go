// Package idempotency makes a handler safe to call many times with the same
// Idempotency-Key, across any number of API replicas.
//
// The lock is a unique index on the key in the store: concurrent duplicates
// race to insert a "pending" record and exactly one wins. Losers get 409 while
// the winner is in flight, and the cached response once it's done. Status codes
// follow the IETF Idempotency-Key header draft.
package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/you/orderguard/internal/httpjson"
)

// Header is the request header carrying the key.
const Header = "Idempotency-Key"

// ReplayedHeader is set on responses served from the cache.
const ReplayedHeader = "Idempotent-Replayed"

// Record statuses.
const (
	StatusPending   = "pending"
	StatusCompleted = "completed"
)

var (
	// ErrExists is returned by Store.Insert when the key is already taken.
	ErrExists = errors.New("idempotency: key exists")
	// ErrNotFound is returned by Store.Get for an unknown key.
	ErrNotFound = errors.New("idempotency: key not found")
)

// Record is one stored key.
type Record struct {
	ID             string
	Key            string
	Fingerprint    string
	Status         string
	ResponseStatus int
	ResponseBody   []byte
	Created        time.Time
}

// Store persists keys. Insert must be atomic on Key (unique index).
type Store interface {
	Insert(ctx context.Context, key, fingerprint string) (id string, err error)
	Get(ctx context.Context, key string) (Record, error)
	Complete(ctx context.Context, id string, status int, body []byte) error
	Delete(ctx context.Context, id string) error
}

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{8,255}$`)

// Middleware wraps next with idempotency handling.
//
// lockTimeout bounds how long a "pending" record is honoured. If the replica
// holding it crashed, a retry after lockTimeout takes the key over. Even then a
// second order can't be written: orders has its own unique index on the key.
// maxBody caps how much of a request body is read for the fingerprint.
func Middleware(store Store, lockTimeout time.Duration, maxBody int64, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get(Header)
			if !keyPattern.MatchString(key) {
				httpjson.Error(w, http.StatusBadRequest, "Idempotency-Key header required (8-255 chars of [A-Za-z0-9_.:-])")
				return
			}
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
			if err != nil {
				httpjson.Error(w, http.StatusRequestEntityTooLarge, "body too large")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			fp := fingerprint(r.Method, r.URL.RequestURI(), body)
			ctx := r.Context()

			id, err := acquire(ctx, store, key, fp, lockTimeout)
			switch {
			case errors.Is(err, ErrExists):
				rec, err := store.Get(ctx, key)
				if errors.Is(err, ErrNotFound) {
					// The owner failed and released the key a moment ago.
					w.Header().Set("Retry-After", "1")
					httpjson.Error(w, http.StatusConflict, "a request with this Idempotency-Key is in progress")
					return
				}
				if err != nil {
					log.Error("idempotency get", "key", key, "err", err)
					httpjson.Error(w, http.StatusInternalServerError, "internal error")
					return
				}
				respondExisting(w, rec, fp)
				return
			case err != nil:
				log.Error("idempotency insert", "key", key, "err", err)
				httpjson.Error(w, http.StatusInternalServerError, "internal error")
				return
			}

			rec := &recorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			// Use a fresh context: the client may have hung up, but the
			// outcome still has to be recorded for the next retry.
			ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if rec.status >= 500 {
				// Failed for reasons the client didn't cause: free the key so
				// a retry runs the handler again.
				if err := store.Delete(ctx, id); err != nil {
					log.Error("idempotency release", "key", key, "err", err)
				}
				return
			}
			if err := store.Complete(ctx, id, rec.status, rec.body.Bytes()); err != nil {
				log.Error("idempotency complete", "key", key, "err", err)
			}
		})
	}
}

// acquire inserts a pending record, taking over one abandoned for longer than
// lockTimeout. Returns ErrExists when another request legitimately owns the key.
func acquire(ctx context.Context, store Store, key, fp string, lockTimeout time.Duration) (string, error) {
	id, err := store.Insert(ctx, key, fp)
	if !errors.Is(err, ErrExists) {
		return id, err
	}
	rec, getErr := store.Get(ctx, key)
	if getErr != nil || rec.Status != StatusPending || time.Since(rec.Created) < lockTimeout || rec.Fingerprint != fp {
		return "", ErrExists
	}
	// Abandoned lock. Several replicas may try this at once; the unique index
	// still lets only one Insert succeed.
	_ = store.Delete(ctx, rec.ID)
	return store.Insert(ctx, key, fp)
}

func respondExisting(w http.ResponseWriter, rec Record, fp string) {
	switch {
	case rec.Fingerprint != fp:
		httpjson.Error(w, http.StatusUnprocessableEntity, "Idempotency-Key reused with a different request")
	case rec.Status == StatusPending:
		w.Header().Set("Retry-After", "1")
		httpjson.Error(w, http.StatusConflict, "a request with this Idempotency-Key is in progress")
	default:
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set(ReplayedHeader, "true")
		w.WriteHeader(rec.ResponseStatus)
		_, _ = w.Write(rec.ResponseBody)
	}
}

func fingerprint(method, uri string, body []byte) string {
	h := sha256.New()
	io.WriteString(h, method+"\n"+uri+"\n")
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// recorder captures status and body while still writing to the client.
type recorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

func (r *recorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status, r.wroteHeader = status, true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(b []byte) (int, error) {
	r.wroteHeader = true
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

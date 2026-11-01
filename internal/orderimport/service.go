package orderimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// ParseError means an uploaded file couldn't be read as a spreadsheet.
type ParseError struct {
	Err error
}

func (e *ParseError) Error() string { return e.Err.Error() }

func (e *ParseError) Unwrap() error { return e.Err }

// Service is what the import endpoints need.
type Service interface {
	// Submit imports data now, or queues it when it is large, in which case the
	// returned Job is not nil.
	Submit(ctx context.Context, userID, companyID string, data []byte) (Summary, *Job, error)
	Job(ctx context.Context, userID, id string) (Job, error)
}

type service struct {
	importer *Importer
	queue    *Queue
	syncRows int
}

// NewService returns a Service. Files of up to syncRows rows are imported while
// the caller waits, larger ones go to queue.
func NewService(importer *Importer, queue *Queue, syncRows int) Service {
	return &service{importer: importer, queue: queue, syncRows: syncRows}
}

func (s *service) Submit(ctx context.Context, userID, companyID string, data []byte) (Summary, *Job, error) {
	rows, skipped, err := Parse(data)
	if errors.Is(err, ErrTooManyRows) {
		return Summary{}, nil, err
	}
	if err != nil {
		return Summary{}, nil, &ParseError{Err: err}
	}

	// The batch id scopes the deterministic keys to this user and this file:
	// the same file re-imported writes each order once; another user's copy is
	// independent.
	sum := sha256.Sum256(data)
	batchID := userID + ":" + hex.EncodeToString(sum[:])

	if len(rows) > s.syncRows {
		job, err := s.queue.Enqueue(ctx, userID, companyID, batchID, data, len(rows))
		if err != nil {
			return Summary{}, nil, err
		}
		return Summary{}, &job, nil
	}
	summary, err := s.importer.Import(ctx, userID, companyID, batchID, rows, skipped)
	return summary, nil, err
}

func (s *service) Job(ctx context.Context, userID, id string) (Job, error) {
	return s.queue.store.UserJob(ctx, userID, id)
}

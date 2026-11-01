package orderimport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Job statuses.
const (
	StatusQueued  = "queued"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
)

// jobRetry is how long the queue waits after the store fails.
const jobRetry = 5 * time.Second

// ErrJobNotFound means there is no such job, or no job waiting.
var ErrJobNotFound = errors.New("orderimport: job not found")

// Job is one spreadsheet waiting in or moving through the queue.
type Job struct {
	ID         string     `json:"id,omitempty"`
	User       string     `json:"user"`
	Company    string     `json:"company"`
	Batch      string     `json:"batch"`
	Spool      string     `json:"spool"` // file name in the spool directory
	Status     string     `json:"status"`
	Total      int        `json:"total"`
	Processed  int        `json:"processed"`
	Imported   int        `json:"imported"`
	Duplicates int        `json:"duplicates"`
	Failed     int        `json:"failed"`
	Skipped    []RowError `json:"skipped"`
}

// JobStore persists jobs, so the queue survives a restart.
type JobStore interface {
	CreateJob(ctx context.Context, j Job) (Job, error)
	UserJob(ctx context.Context, user, id string) (Job, error)
	NextJob(ctx context.Context) (Job, error) // oldest unfinished, or ErrJobNotFound
	UpdateJob(ctx context.Context, j Job) error
}

// Queue runs imports one at a time, oldest first. A job interrupted by a restart
// runs again from the top, and its rows' idempotency keys stop it writing any
// order twice. It expects a single API replica.
type Queue struct {
	store    JobStore
	importer *Importer
	dir      string
	wake     chan struct{}
	log      *slog.Logger
}

// NewQueue returns a Queue that keeps waiting files in dir.
func NewQueue(store JobStore, importer *Importer, dir string, log *slog.Logger) (*Queue, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Queue{store: store, importer: importer, dir: dir, wake: make(chan struct{}, 1), log: log}, nil
}

// Enqueue saves data and adds it to the back of the queue.
func (q *Queue) Enqueue(ctx context.Context, userID, companyID, batchID string, data []byte, total int) (Job, error) {
	name := make([]byte, 16)
	rand.Read(name)
	spool := hex.EncodeToString(name)
	path := filepath.Join(q.dir, spool)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return Job{}, err
	}
	job, err := q.store.CreateJob(ctx, Job{User: userID, Company: companyID, Batch: batchID, Spool: spool, Status: StatusQueued, Total: total})
	if err != nil {
		os.Remove(path)
		return Job{}, err
	}
	select {
	case q.wake <- struct{}{}:
	default: // already awake, or about to look
	}
	return job, nil
}

// Run processes jobs until ctx ends.
func (q *Queue) Run(ctx context.Context) {
	for ctx.Err() == nil {
		job, err := q.store.NextJob(ctx)
		switch {
		case errors.Is(err, ErrJobNotFound):
			select {
			case <-q.wake:
			case <-ctx.Done():
			}
			continue
		case err == nil:
			err = q.process(ctx, job)
		}
		if err != nil && ctx.Err() == nil {
			q.log.Error("import job", "id", job.ID, "err", err)
			select {
			case <-time.After(jobRetry):
			case <-ctx.Done():
			}
		}
	}
}

// process runs one job. An error leaves the job unfinished, so it runs again.
func (q *Queue) process(ctx context.Context, job Job) error {
	path := filepath.Join(q.dir, job.Spool)
	job.Status = StatusRunning
	job.Processed, job.Imported, job.Duplicates, job.Failed = 0, 0, 0, 0
	if err := q.store.UpdateJob(ctx, job); err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return q.finish(ctx, job, StatusFailed, err)
	}
	rows, skipped, err := Parse(data)
	if err != nil {
		return q.finish(ctx, job, StatusFailed, err)
	}

	// One goroutine saves progress, so the workers never wait on it and a late
	// update can't overwrite a newer one.
	report := make(chan Summary, 1)
	var saver sync.WaitGroup
	saver.Go(func() {
		for s := range report {
			update := job
			update.Processed, update.Imported, update.Duplicates, update.Failed = s.Total, s.Created, s.Duplicates, s.Failed
			if err := q.store.UpdateJob(ctx, update); err != nil && ctx.Err() == nil {
				q.log.Error("import progress", "id", job.ID, "err", err)
			}
		}
	})
	s, err := q.importer.stream(ctx, job.User, job.Company, job.Batch, rows, skipped[:min(len(skipped), maxSampledErrors)], report)
	close(report)
	saver.Wait()
	if err != nil {
		return err
	}

	job.Processed, job.Imported, job.Duplicates, job.Failed, job.Skipped = s.Total, s.Created, s.Duplicates, s.Failed, s.Skipped
	return q.finish(ctx, job, StatusDone, nil)
}

func (q *Queue) finish(ctx context.Context, job Job, status string, cause error) error {
	if cause != nil {
		q.log.Error("import job failed", "id", job.ID, "err", cause)
	}
	job.Status = status
	// Don't let a shutdown lose the outcome of finished work.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := q.store.UpdateJob(ctx, job); err != nil {
		return err
	}
	// The outcome is saved, so a leftover file must not hold up the next job.
	if err := os.Remove(filepath.Join(q.dir, job.Spool)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		q.log.Error("remove import file", "id", job.ID, "err", err)
	}
	return nil
}

package pocketbase

import (
	"context"
	"errors"

	"github.com/you/orderguard/internal/orderimport"
)

const importJobsCollection = "import_jobs"

// ImportJobs implements orderimport.JobStore.
type ImportJobs struct {
	pb *Client
}

// NewImportJobs returns an ImportJobs using pb.
func NewImportJobs(pb *Client) *ImportJobs {
	return &ImportJobs{pb: pb}
}

// CreateJob inserts j.
func (r *ImportJobs) CreateJob(ctx context.Context, j orderimport.Job) (orderimport.Job, error) {
	var out orderimport.Job
	err := r.pb.Create(ctx, importJobsCollection, j, &out)
	return out, err
}

// UserJob returns one of the user's jobs.
func (r *ImportJobs) UserJob(ctx context.Context, user, id string) (orderimport.Job, error) {
	j, err := First[orderimport.Job](ctx, r.pb, importJobsCollection, "id="+Quote(id)+" && user="+Quote(user))
	if errors.Is(err, ErrNotFound) {
		return orderimport.Job{}, orderimport.ErrJobNotFound
	}
	return j, err
}

// NextJob returns the oldest unfinished job. A job left running by a restart
// counts, and being oldest it resumes first.
func (r *ImportJobs) NextJob(ctx context.Context) (orderimport.Job, error) {
	filter := "status=" + Quote(orderimport.StatusQueued) + " || status=" + Quote(orderimport.StatusRunning)
	page, err := List[orderimport.Job](ctx, r.pb, importJobsCollection, filter, "created", 1)
	if err != nil {
		return orderimport.Job{}, err
	}
	if len(page.Items) == 0 {
		return orderimport.Job{}, orderimport.ErrJobNotFound
	}
	return page.Items[0], nil
}

// UpdateJob saves j's status and counts.
func (r *ImportJobs) UpdateJob(ctx context.Context, j orderimport.Job) error {
	id := j.ID
	j.ID = ""
	return r.pb.Update(ctx, importJobsCollection, id, j, nil)
}

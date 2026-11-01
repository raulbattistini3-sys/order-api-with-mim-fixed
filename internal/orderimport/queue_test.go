package orderimport

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/you/orderguard/internal/auth"
)

// memJobs keeps jobs in insertion order, so index order is FIFO order.
type memJobs struct {
	mu   sync.Mutex
	jobs []Job
	ran  []string // job ids, in the order they first went running
	seen []int    // Processed, on each update of a running job
}

func (s *memJobs) CreateJob(_ context.Context, j Job) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j.ID = fmt.Sprintf("job%d", len(s.jobs))
	s.jobs = append(s.jobs, j)
	return j, nil
}

func (s *memJobs) UserJob(_ context.Context, user, id string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.ID == id && j.User == user {
			return j, nil
		}
	}
	return Job{}, ErrJobNotFound
}

func (s *memJobs) NextJob(context.Context) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.Status == StatusQueued || j.Status == StatusRunning {
			return j, nil
		}
	}
	return Job{}, ErrJobNotFound
}

func (s *memJobs) UpdateJob(_ context.Context, j Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.jobs {
		if s.jobs[i].ID == j.ID {
			if s.jobs[i].Status == StatusQueued && j.Status == StatusRunning {
				s.ran = append(s.ran, j.ID)
			}
			if j.Status == StatusRunning {
				s.seen = append(s.seen, j.Processed)
			}
			s.jobs[i] = j
		}
	}
	return nil
}

func (s *memJobs) settled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.Status == StatusQueued || j.Status == StatusRunning {
			return false
		}
	}
	return true
}

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newQueue(t *testing.T, store JobStore, repo *fakeRepo) (*Queue, string) {
	t.Helper()
	dir := t.TempDir()
	q, err := NewQueue(store, NewImporter(repo, 4, 50), dir, discardLog())
	if err != nil {
		t.Fatal(err)
	}
	return q, dir
}

func run(t *testing.T, q *Queue, store *memJobs) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { q.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	for deadline := time.Now().Add(5 * time.Second); !store.settled(); {
		if time.Now().After(deadline) {
			t.Fatalf("jobs never finished: %+v", store.jobs)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestQueueRunsJobsOldestFirst(t *testing.T) {
	store, repo := &memJobs{}, &fakeRepo{keys: map[string]bool{}}
	q, dir := newQueue(t, store, repo)
	ctx := context.Background()
	for i := range 3 {
		csv := fmt.Sprintf("item,quantity\nA%d,1\nB%d,2\nbad,x\n", i, i)
		if _, err := q.Enqueue(ctx, "u", "c", fmt.Sprintf("batch%d", i), []byte(csv), 2); err != nil {
			t.Fatal(err)
		}
	}
	run(t, q, store)

	if got := strings.Join(store.ran, ","); got != "job0,job1,job2" {
		t.Fatalf("ran = %s", got)
	}
	for _, j := range store.jobs {
		if j.Status != StatusDone || j.Imported != 2 || j.Processed != 2 || len(j.Skipped) != 1 {
			t.Fatalf("job = %+v", j)
		}
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Fatalf("%d spool files left", len(left))
	}
}

func TestQueueReportsProgressWhileRunning(t *testing.T) {
	store, repo := &memJobs{}, &fakeRepo{keys: map[string]bool{}}
	q, _ := newQueue(t, store, repo)
	var csv strings.Builder
	csv.WriteString("item,quantity\n")
	for i := range 2500 {
		fmt.Fprintf(&csv, "item%d,1\n", i)
	}
	if _, err := q.Enqueue(context.Background(), "u", "c", "b", []byte(csv.String()), 2500); err != nil {
		t.Fatal(err)
	}
	run(t, q, store)

	if j := store.jobs[0]; j.Status != StatusDone || j.Imported != 2500 || j.Processed != 2500 {
		t.Fatalf("job = %+v", j)
	}
	if !slices.ContainsFunc(store.seen, func(n int) bool { return n >= reportEvery && n < 2500 }) {
		t.Fatalf("no progress saved mid-job: %v", store.seen)
	}
}

// A restart leaves a job running with some rows already written.
func TestQueueResumesInterruptedJobWithoutDuplicates(t *testing.T) {
	store, repo := &memJobs{}, &fakeRepo{keys: map[string]bool{}}
	q, dir := newQueue(t, store, repo)
	data := []byte("item,quantity\nA,1\nB,2\nC,3\n")
	if err := os.WriteFile(filepath.Join(dir, "f"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	rows, _, _ := Parse(data)
	if _, err := q.importer.Import(context.Background(), "u", "c", "b", rows[:2], nil); err != nil {
		t.Fatal(err)
	}
	store.CreateJob(context.Background(), Job{User: "u", Batch: "b", Spool: "f", Status: StatusRunning, Total: 3, Processed: 2})

	run(t, q, store)

	if j := store.jobs[0]; j.Status != StatusDone || j.Imported != 1 || j.Duplicates != 2 {
		t.Fatalf("job = %+v", j)
	}
	if len(repo.keys) != 3 {
		t.Fatalf("orders = %d", len(repo.keys))
	}
}

func TestQueueFailsJobWhoseFileIsGoneAndMovesOn(t *testing.T) {
	store, repo := &memJobs{}, &fakeRepo{keys: map[string]bool{}}
	q, _ := newQueue(t, store, repo)
	store.CreateJob(context.Background(), Job{User: "u", Batch: "b0", Spool: "missing", Status: StatusQueued})
	if _, err := q.Enqueue(context.Background(), "u", "c", "b1", []byte("item,quantity\nA,1\n"), 1); err != nil {
		t.Fatal(err)
	}
	run(t, q, store)

	if store.jobs[0].Status != StatusFailed || store.jobs[1].Status != StatusDone {
		t.Fatalf("jobs = %+v", store.jobs)
	}
}

func importRequest(method, target, user, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set(auth.UserHeader, user)
	req.Header.Set(auth.CompanyHeader, "c")
	return req
}

func TestHandlerSendsLargeFilesToTheQueue(t *testing.T) {
	store, repo := &memJobs{}, &fakeRepo{keys: map[string]bool{}}
	q, _ := newQueue(t, store, repo)
	mux := http.NewServeMux()
	Routes(mux, NewService(q.importer, q, 2), 1<<20, discardLog())

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, importRequest(http.MethodPost, "/me/orders/import", "u", "item,quantity\nA,1\nB,2\n"))
	if rec.Code != http.StatusOK || len(repo.keys) != 2 {
		t.Fatalf("at threshold: status = %d, orders = %d", rec.Code, len(repo.keys))
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, importRequest(http.MethodPost, "/me/orders/import", "u", "item,quantity\nC,1\nD,2\nE,3\n"))
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"id":"job0"`) || strings.Contains(rec.Body.String(), "spool") {
		t.Fatalf("over threshold: %d %s", rec.Code, rec.Body)
	}
	if len(repo.keys) != 2 {
		t.Fatalf("wrote %d orders before the queue ran", len(repo.keys))
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, importRequest(http.MethodGet, "/me/orders/import/job0", "u", ""))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"queued"`) {
		t.Fatalf("own job: %d %s", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, importRequest(http.MethodGet, "/me/orders/import/job0", "someone-else", ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("another user's job: %d", rec.Code)
	}
}

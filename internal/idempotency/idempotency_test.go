package idempotency

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// memStore mimics a unique index with a mutex.
type memStore struct {
	mu   sync.Mutex
	recs map[string]*Record
	seq  int
}

func newMemStore() *memStore { return &memStore{recs: map[string]*Record{}} }

func (m *memStore) Insert(_ context.Context, key, fp string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.recs[key]; ok {
		return "", ErrExists
	}
	m.seq++
	id := strconv.Itoa(m.seq)
	m.recs[key] = &Record{ID: id, Key: key, Fingerprint: fp, Status: StatusPending, Created: time.Now()}
	return id, nil
}

func (m *memStore) Get(_ context.Context, key string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.recs[key]
	if !ok {
		return Record{}, ErrNotFound
	}
	return *r, nil
}

func (m *memStore) Complete(_ context.Context, id string, status int, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.recs {
		if r.ID == id {
			r.Status, r.ResponseStatus, r.ResponseBody = StatusCompleted, status, body
		}
	}
	return nil
}

func (m *memStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, r := range m.recs {
		if r.ID == id {
			delete(m.recs, k)
		}
	}
	return nil
}

func newServer(t *testing.T, store Store, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(Middleware(store, 30*time.Second, log)(handler))
	t.Cleanup(srv.Close)
	return srv
}

func post(t *testing.T, url, key, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set(Header, key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestConcurrentDuplicatesRunHandlerOnce(t *testing.T) {
	var calls atomic.Int32
	srv := newServer(t, newMemStore(), func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(50 * time.Millisecond) // hold the lock so duplicates collide
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"id":"order-1"}`)
	})

	var wg sync.WaitGroup
	statuses := make([]int, 20)
	for i := range statuses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := post(t, srv.URL+"/orders", "key-00000001", `{"q":1}`)
			res.Body.Close()
			statuses[i] = res.StatusCode
		}()
	}
	wg.Wait()

	if n := calls.Load(); n != 1 {
		t.Fatalf("handler ran %d times, want 1 (statuses %v)", n, statuses)
	}
	for _, s := range statuses {
		if s != http.StatusCreated && s != http.StatusConflict {
			t.Fatalf("unexpected status %d in %v", s, statuses)
		}
	}

	// Once finished, a retry gets the cached response.
	res := post(t, srv.URL+"/orders", "key-00000001", `{"q":1}`)
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusCreated || res.Header.Get(ReplayedHeader) != "true" || string(body) != `{"id":"order-1"}` {
		t.Fatalf("replay = %d %q replayed=%q", res.StatusCode, body, res.Header.Get(ReplayedHeader))
	}
}

func TestSameKeyDifferentBody(t *testing.T) {
	srv := newServer(t, newMemStore(), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	post(t, srv.URL, "key-00000001", `{"q":1}`).Body.Close()
	res := post(t, srv.URL, "key-00000001", `{"q":100}`)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", res.StatusCode)
	}
}

func TestServerErrorReleasesKey(t *testing.T) {
	var calls atomic.Int32
	srv := newServer(t, newMemStore(), func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
	post(t, srv.URL, "key-00000001", `{}`).Body.Close()
	res := post(t, srv.URL, "key-00000001", `{}`)
	if res.StatusCode != http.StatusCreated || calls.Load() != 2 {
		t.Fatalf("retry after 500: status %d, calls %d", res.StatusCode, calls.Load())
	}
}

func TestMissingKey(t *testing.T) {
	srv := newServer(t, newMemStore(), func(w http.ResponseWriter, r *http.Request) {})
	res := post(t, srv.URL, "", `{}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", res.StatusCode)
	}
}

package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/you/orderguard/internal/order"
)

// fakeMeili answers the calls Client makes, the way the docs describe them. Each
// write is queued, and reports "processing" once before it settles.
type fakeMeili struct {
	mu         sync.Mutex
	settings   int
	docs       map[string]document
	tasks      map[int]string
	polled     map[int]bool
	lastSearch map[string]any
	key        string
	down       bool
}

func (f *fakeMeili) queue(w http.ResponseWriter, failure string) {
	id := len(f.tasks) + 1
	f.tasks[id] = failure
	w.WriteHeader(http.StatusAccepted)
	fmt.Fprintf(w, "{\"taskUid\":%d}", id)
}

func (f *fakeMeili) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+f.key {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	switch {
	case r.Method == http.MethodPatch && r.URL.Path == "/indexes/orders/settings":
		f.settings++
		f.queue(w, "")
	case r.Method == http.MethodPost && r.URL.Path == "/indexes/orders/documents":
		if r.URL.Query().Get("primaryKey") != "id" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var docs []document
		json.NewDecoder(r.Body).Decode(&docs)
		failure := ""
		for _, d := range docs {
			if d.Item == "reject" {
				failure = "bad document"
			}
		}
		if failure == "" {
			for _, d := range docs {
				f.docs[d.ID] = d
			}
		}
		f.queue(w, failure)
	case r.Method == http.MethodPost && r.URL.Path == "/indexes/orders/search":
		json.NewDecoder(r.Body).Decode(&f.lastSearch)
		hits := []document{}
		for _, d := range f.docs {
			hits = append(hits, d)
		}
		slices.SortFunc(hits, func(a, b document) int { return int(b.CreatedMS - a.CreatedMS) })
		if len(hits) > 1 {
			hits = hits[:1]
		}
		json.NewEncoder(w).Encode(map[string][]document{"hits": hits})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/tasks/"):
		id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/tasks/"))
		failure, known := f.tasks[id]
		switch {
		case !known:
			w.WriteHeader(http.StatusNotFound)
		case !f.polled[id]:
			f.polled[id] = true
			fmt.Fprint(w, "{\"status\":\"processing\"}")
		case failure != "":
			fmt.Fprintf(w, "{\"status\":\"failed\",\"error\":{\"message\":%q}}", failure)
		default:
			fmt.Fprint(w, "{\"status\":\"succeeded\"}")
		}
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func setup(t *testing.T) (*fakeMeili, *Client) {
	t.Helper()
	fake := &fakeMeili{docs: map[string]document{}, tasks: map[int]string{}, polled: map[int]bool{}, key: "k"}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	return fake, New(srv.URL+"/", "k", "orders")
}

func created(n int) string {
	return time.UnixMilli(int64(n) + 1_700_000_000_000).UTC().Format(createdLayout)
}

func orderN(n int) order.Order {
	return order.Order{ID: fmt.Sprintf("o%05d", n), Customer: "u1", Item: "item", Quantity: 2, Created: created(n)}
}

type memSource struct {
	mu     sync.Mutex
	orders []order.Order
	reads  []*order.Cursor
}

func (s *memSource) ListSince(_ context.Context, after *order.Cursor, limit int) ([]order.Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads = append(s.reads, after)
	var out []order.Order
	for _, o := range s.orders {
		newer := after == nil || o.Created > after.Created || (o.Created == after.Created && o.ID > after.ID)
		if newer && len(out) < limit {
			out = append(out, o)
		}
	}
	return out, nil
}

func indexer(c *Client, s Source) *Indexer {
	return NewIndexer(c, s, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestEnsureToleratesAnExistingIndex(t *testing.T) {
	_, c := setup(t)
	for range 2 {
		if err := c.Ensure(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEnsureReportsARejectedKey(t *testing.T) {
	fake, c := setup(t)
	fake.key = "other"
	if err := c.Ensure(context.Background()); err == nil {
		t.Fatal("want error")
	}
}

func TestUpsertReportsARejectedDocument(t *testing.T) {
	_, c := setup(t)
	bad := orderN(1)
	bad.Item = "reject"
	err := c.Upsert(context.Background(), []order.Order{orderN(0), bad})
	if err == nil || !strings.Contains(err.Error(), "bad document") {
		t.Fatalf("err = %v", err)
	}
}

func TestSearchAlwaysFiltersByCustomer(t *testing.T) {
	fake, c := setup(t)
	if err := c.Upsert(context.Background(), []order.Order{orderN(1)}); err != nil {
		t.Fatal(err)
	}
	got, err := c.Search(context.Background(), `u"1 OR customer = x`, "tecl", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "o00001" || got[0].Quantity != 2 || got[0].Created != created(1) {
		t.Fatalf("got = %+v", got)
	}
	if want := `customer = "u\"1 OR customer = x"`; fake.lastSearch["filter"] != want {
		t.Fatalf("filter = %q", fake.lastSearch["filter"])
	}
	if fake.lastSearch["q"] != "tecl" || fake.lastSearch["limit"] != 5.0 {
		t.Fatalf("params = %v", fake.lastSearch)
	}
}

func TestStepBackfillsInBatches(t *testing.T) {
	fake, c := setup(t)
	source := &memSource{}
	for n := range indexBatch + 5 {
		source.orders = append(source.orders, orderN(n))
	}
	ctx := context.Background()
	ix := indexer(c, source)

	after, more, err := ix.step(ctx, nil)
	if err != nil || !more || len(fake.docs) != indexBatch {
		t.Fatalf("first: more = %v, docs = %d, err = %v", more, len(fake.docs), err)
	}
	after, more, err = ix.step(ctx, after)
	if err != nil || more || len(fake.docs) != indexBatch+5 {
		t.Fatalf("second: more = %v, docs = %d, err = %v", more, len(fake.docs), err)
	}
	if again, _, _ := ix.step(ctx, after); again != after {
		t.Fatalf("an idle step moved the cursor to %+v", again)
	}
}

func TestStepKeepsItsPlaceWhenMeilisearchIsDown(t *testing.T) {
	fake, c := setup(t)
	source := &memSource{orders: []order.Order{orderN(0)}}
	ix := indexer(c, source)
	fake.down = true
	after, _, err := ix.step(context.Background(), nil)
	if err == nil || after != nil {
		t.Fatalf("after = %+v, err = %v", after, err)
	}
	fake.down = false
	if _, _, err := ix.step(context.Background(), after); err != nil || len(fake.docs) != 1 {
		t.Fatalf("docs = %d, err = %v", len(fake.docs), err)
	}
}

func TestRestartResumesFromTheNewestIndexedOrder(t *testing.T) {
	fake, c := setup(t)
	ctx := context.Background()
	if err := c.Upsert(ctx, []order.Order{orderN(1), orderN(7)}); err != nil {
		t.Fatal(err)
	}
	after, err := indexer(c, &memSource{}).start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after == nil || after.Created != created(7) || after.ID != "" {
		t.Fatalf("after = %+v", after)
	}
	if fake.settings != 1 {
		t.Fatalf("settings = %d", fake.settings)
	}
}

func TestRunBackfillsThenStopsWithItsContext(t *testing.T) {
	fake, c := setup(t)
	source := &memSource{orders: []order.Order{orderN(0), orderN(1)}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { indexer(c, source).Run(ctx); close(done) }()

	for deadline := time.Now().Add(5 * time.Second); ; {
		fake.mu.Lock()
		n := len(fake.docs)
		fake.mu.Unlock()
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("orders never reached the index")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal(errors.New("Run ignored its context"))
	}
}

// Package search keeps orders searchable in a self-hosted Meilisearch. Client
// speaks its REST API, and Indexer copies orders into it from the store, so
// imports, older orders and writes made while Meilisearch was down all arrive
// the same way.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/you/orderguard/internal/order"
)

const (
	indexBatch = 1000
	idlePoll   = 2 * time.Second
	errorPoll  = 5 * time.Second
	taskPoll   = 100 * time.Millisecond
	// taskTimeout bounds the wait for one write. Writes can be repeated safely,
	// so running out only costs a retry.
	taskTimeout = 2 * time.Minute
	// createdLayout is how PocketBase writes timestamps.
	createdLayout = "2006-01-02 15:04:05.000Z"
)

// document is one order as Meilisearch stores it.
type document struct {
	ID        string `json:"id"`
	Customer  string `json:"customer"`
	Item      string `json:"item"`
	Quantity  int    `json:"quantity"`
	Created   string `json:"created"`
	CreatedMS int64  `json:"created_ms"`
}

// Client talks to one Meilisearch index.
type Client struct {
	baseURL string
	apiKey  string
	index   string
	http    *http.Client
}

// New returns a Client for index.
func New(baseURL, apiKey, index string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		index:   index,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("search: %s %s: %d %s", method, path, res.StatusCode, raw)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (c *Client) indexPath(suffix string) string {
	return "/indexes/" + url.PathEscape(c.index) + suffix
}

// write sends a change and waits for it. Meilisearch only queues a write, and a
// bad one fails later, so the answer to the request says nothing about it.
func (c *Client) write(ctx context.Context, method, path string, in any) error {
	var task struct {
		UID int64 `json:"taskUid"`
	}
	if err := c.call(ctx, method, path, in, &task); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, taskTimeout)
	defer cancel()
	for {
		var state struct {
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := c.call(ctx, http.MethodGet, fmt.Sprintf("/tasks/%d", task.UID), nil, &state); err != nil {
			return err
		}
		switch state.Status {
		case "succeeded":
			return nil
		case "failed", "canceled":
			if state.Error != nil {
				return fmt.Errorf("search: task %d: %s", task.UID, state.Error.Message)
			}
			return fmt.Errorf("search: task %d: %s", task.UID, state.Status)
		}
		select {
		case <-time.After(taskPoll):
		case <-ctx.Done():
			return fmt.Errorf("search: task %d: %w", task.UID, ctx.Err())
		}
	}
}

// Ensure creates the index if it isn't there yet and applies its settings.
// Setting what's already set changes nothing.
func (c *Client) Ensure(ctx context.Context) error {
	return c.write(ctx, http.MethodPatch, c.indexPath("/settings"), map[string]any{
		"searchableAttributes": []string{"item"},
		"filterableAttributes": []string{"customer"},
		"sortableAttributes":   []string{"created_ms"},
	})
}

// Upsert stores orders. Writing one twice changes nothing.
func (c *Client) Upsert(ctx context.Context, orders []order.Order) error {
	docs := make([]document, len(orders))
	for i, o := range orders {
		created, err := time.Parse(createdLayout, o.Created)
		if err != nil {
			return fmt.Errorf("search: order %s: %w", o.ID, err)
		}
		docs[i] = document{ID: o.ID, Customer: o.Customer, Item: o.Item, Quantity: o.Quantity, Created: o.Created, CreatedMS: created.UnixMilli()}
	}
	return c.write(ctx, http.MethodPost, c.indexPath("/documents?primaryKey=id"), docs)
}

func (c *Client) query(ctx context.Context, params map[string]any) ([]document, error) {
	var result struct {
		Hits []document `json:"hits"`
	}
	err := c.call(ctx, http.MethodPost, c.indexPath("/search"), params, &result)
	return result.Hits, err
}

var filterQuotes = strings.NewReplacer("\\", "\\\\", "\"", "\\\"")

// Search returns customer's best matches for q, newest first among equals. The
// customer filter is what keeps one user out of another's orders, so it never
// comes from the client.
func (c *Client) Search(ctx context.Context, customer, q string, limit int) ([]order.Order, error) {
	hits, err := c.query(ctx, map[string]any{
		"q":      q,
		"filter": "customer = \"" + filterQuotes.Replace(customer) + "\"",
		"sort":   []string{"created_ms:desc"},
		"limit":  limit,
	})
	if err != nil {
		return nil, err
	}
	orders := make([]order.Order, len(hits))
	for i, d := range hits {
		orders[i] = order.Order{ID: d.ID, Customer: d.Customer, Item: d.Item, Quantity: d.Quantity, Created: d.Created}
	}
	return orders, nil
}

// newest returns the timestamp of the latest indexed order, or "" for none.
func (c *Client) newest(ctx context.Context) (string, error) {
	hits, err := c.query(ctx, map[string]any{"q": "", "sort": []string{"created_ms:desc"}, "limit": 1})
	if err != nil || len(hits) == 0 {
		return "", err
	}
	return hits[0].Created, nil
}

// Source reads every customer's orders, oldest first.
type Source interface {
	ListSince(ctx context.Context, after *order.Cursor, limit int) ([]order.Order, error)
}

// Indexer copies orders from a Source into Typesense.
type Indexer struct {
	client *Client
	source Source
	log    *slog.Logger
}

// NewIndexer returns an Indexer.
func NewIndexer(client *Client, source Source, log *slog.Logger) *Indexer {
	return &Indexer{client: client, source: source, log: log}
}

// Run indexes until ctx ends. It starts from the newest order Typesense has, so
// a restart or an outage only costs the time to catch up. It's a separate loop
// so a search outage can never fail a write.
func (i *Indexer) Run(ctx context.Context) {
	var after *order.Cursor
	ready := false
	for ctx.Err() == nil {
		wait := idlePoll
		var err error
		if !ready {
			after, err = i.start(ctx)
			ready = err == nil
		}
		if ready {
			var more bool
			after, more, err = i.step(ctx, after)
			if more {
				wait = 0
			}
		}
		if err != nil {
			if ctx.Err() == nil {
				i.log.Error("search index", "err", err)
			}
			wait = errorPoll
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
		}
	}
}

func (i *Indexer) start(ctx context.Context) (*order.Cursor, error) {
	if err := i.client.Ensure(ctx); err != nil {
		return nil, err
	}
	created, err := i.client.newest(ctx)
	if err != nil || created == "" {
		return nil, err
	}
	// An empty id reads that whole millisecond again, which upserts make safe.
	return &order.Cursor{Created: created}, nil
}

// step copies one batch and says whether more may be waiting.
func (i *Indexer) step(ctx context.Context, after *order.Cursor) (*order.Cursor, bool, error) {
	orders, err := i.source.ListSince(ctx, after, indexBatch)
	if err != nil {
		return after, false, err
	}
	if len(orders) == 0 {
		return after, false, nil
	}
	if err := i.client.Upsert(ctx, orders); err != nil {
		return after, false, err
	}
	last := orders[len(orders)-1]
	return &order.Cursor{Created: last.Created, ID: last.ID}, len(orders) == indexBatch, nil
}

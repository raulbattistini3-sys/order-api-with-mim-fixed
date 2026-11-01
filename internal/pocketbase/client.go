// Package pocketbase is a minimal REST client for PocketBase, authenticated as
// a superuser. It covers only what this service needs: create, update, delete
// and filtered list on base collections.
package pocketbase

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// ErrNotUnique is returned when a write violates a unique index.
	ErrNotUnique = errors.New("pocketbase: unique constraint violation")
	// ErrNotFound is returned for 404 responses.
	ErrNotFound = errors.New("pocketbase: not found")
)

// APIError is a non-2xx response from PocketBase.
type APIError struct {
	Status  int                          `json:"status"`
	Message string                       `json:"message"`
	Data    map[string]map[string]string `json:"data"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("pocketbase: %d %s %v", e.Status, e.Message, e.Data)
}

// Is lets callers use errors.Is(err, ErrNotUnique) / errors.Is(err, ErrNotFound).
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.Status == http.StatusNotFound
	case ErrNotUnique:
		for _, field := range e.Data {
			if field["code"] == "validation_not_unique" {
				return true
			}
		}
	}
	return false
}

// Client talks to a single PocketBase instance.
type Client struct {
	baseURL  string
	email    string
	password string
	http     *http.Client

	mu    sync.Mutex
	token string
}

// New returns a Client. Authentication happens lazily on the first call.
func New(baseURL, email, password string) *Client {
	// Reuse connections to PocketBase: the default 2 idle per host would make
	// an import or a load test open a socket per write and exhaust ports.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 256
	transport.MaxIdleConnsPerHost = 128
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		email:    email,
		password: password,
		http:     &http.Client{Timeout: 10 * time.Second, Transport: transport},
	}
}

// Create inserts a record and decodes the stored record into out.
func (c *Client) Create(ctx context.Context, collection string, in, out any) error {
	return c.do(ctx, http.MethodPost, recordsPath(collection), in, out)
}

// Update patches a record by id and decodes the stored record into out.
func (c *Client) Update(ctx context.Context, collection, id string, in, out any) error {
	return c.do(ctx, http.MethodPatch, recordsPath(collection)+"/"+url.PathEscape(id), in, out)
}

// Get reads a record by id into out.
func (c *Client) Get(ctx context.Context, collection, id string, out any) error {
	return c.do(ctx, http.MethodGet, recordsPath(collection)+"/"+url.PathEscape(id), nil, out)
}

// Public sends one request without superuser credentials, using token if it
// isn't empty. It is for endpoints that act as a user rather than as this
// service.
func (c *Client) Public(ctx context.Context, method, path, token string, in, out any) error {
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return err
		}
	}
	_, err := c.send(ctx, method, path, token, body, out)
	return err
}

// Delete removes a record by id.
func (c *Client) Delete(ctx context.Context, collection, id string) error {
	return c.do(ctx, http.MethodDelete, recordsPath(collection)+"/"+url.PathEscape(id), nil, nil)
}

// Page is one page of a list response.
type Page[T any] struct {
	Items      []T `json:"items"`
	TotalItems int `json:"totalItems"`
}

// BatchCreate inserts records in one transaction, so either all are written or
// none are. The batch API must be enabled in PocketBase's settings.
func BatchCreate[T any](ctx context.Context, c *Client, collection string, records []T) error {
	type request struct {
		Method string `json:"method"`
		URL    string `json:"url"`
		Body   T      `json:"body"`
	}
	requests := make([]request, len(records))
	for i, record := range records {
		requests[i] = request{Method: http.MethodPost, URL: recordsPath(collection), Body: record}
	}
	return c.do(ctx, http.MethodPost, "/api/batch", map[string]any{"requests": requests}, nil)
}

// Scroll is List without the total, which PocketBase would have to count on
// every page.
func Scroll[T any](ctx context.Context, c *Client, collection, filter, sort string, perPage int) ([]T, error) {
	q := url.Values{}
	q.Set("filter", filter)
	q.Set("sort", sort)
	q.Set("perPage", strconv.Itoa(perPage))
	q.Set("skipTotal", "1")
	var page Page[T]
	err := c.do(ctx, http.MethodGet, recordsPath(collection)+"?"+q.Encode(), nil, &page)
	return page.Items, err
}

// List runs a filtered list query. filter uses PocketBase filter syntax; build
// string literals with Quote.
func List[T any](ctx context.Context, c *Client, collection, filter, sort string, perPage int) (Page[T], error) {
	q := url.Values{}
	if filter != "" {
		q.Set("filter", filter)
	}
	if sort != "" {
		q.Set("sort", sort)
	}
	q.Set("perPage", strconv.Itoa(perPage))
	var page Page[T]
	err := c.do(ctx, http.MethodGet, recordsPath(collection)+"?"+q.Encode(), nil, &page)
	return page, err
}

// First returns the first record matching filter, or ErrNotFound.
func First[T any](ctx context.Context, c *Client, collection, filter string) (T, error) {
	var zero T
	page, err := List[T](ctx, c, collection, filter, "", 1)
	if err != nil {
		return zero, err
	}
	if len(page.Items) == 0 {
		return zero, ErrNotFound
	}
	return page.Items[0], nil
}

// Quote turns s into a PocketBase filter string literal.
func Quote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return "'" + strings.ReplaceAll(s, "'", `\'`) + "'"
}

func recordsPath(collection string) string {
	return "/api/collections/" + url.PathEscape(collection) + "/records"
}

// do sends one request, authenticating first if needed and once more if the
// token was rejected (expired or PocketBase restarted with a new secret).
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return err
		}
	}
	for attempt := 0; ; attempt++ {
		token, err := c.authToken(ctx, attempt > 0)
		if err != nil {
			return err
		}
		status, err := c.send(ctx, method, path, token, body, out)
		if attempt == 0 && (status == http.StatusUnauthorized || status == http.StatusForbidden) {
			continue
		}
		return err
	}
}

func (c *Client) send(ctx context.Context, method, path, token string, body []byte, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return res.StatusCode, err
	}
	if res.StatusCode >= 300 {
		apiErr := &APIError{Status: res.StatusCode}
		_ = json.Unmarshal(raw, apiErr)
		return res.StatusCode, apiErr
	}
	if out != nil && len(raw) > 0 {
		return res.StatusCode, json.Unmarshal(raw, out)
	}
	return res.StatusCode, nil
}

func (c *Client) authToken(ctx context.Context, refresh bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && !refresh {
		return c.token, nil
	}
	body, _ := json.Marshal(map[string]string{"identity": c.email, "password": c.password})
	var res struct {
		Token string `json:"token"`
	}
	if _, err := c.send(ctx, http.MethodPost, "/api/collections/_superusers/auth-with-password", "", body, &res); err != nil {
		return "", fmt.Errorf("pocketbase auth: %w", err)
	}
	c.token = res.Token
	return c.token, nil
}

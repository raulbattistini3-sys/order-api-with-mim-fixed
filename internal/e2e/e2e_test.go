//go:build e2e

// Package e2e drives the real gateway and API against a real PocketBase in a
// temporary directory. Only the CNPJ registry is faked, so the suite needs no
// network. It needs the PocketBase binary, which `node scripts/run.mjs` fetches:
//
//	go test -tags e2e ./internal/e2e/
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/you/orderguard/internal/api"
	"github.com/you/orderguard/internal/auth"
	"github.com/you/orderguard/internal/company"
	"github.com/you/orderguard/internal/gateway"
	"github.com/you/orderguard/internal/order"
	"github.com/you/orderguard/internal/orderimport"
	"github.com/you/orderguard/internal/pocketbase"
	"github.com/you/orderguard/internal/ratelimit"
	"github.com/you/orderguard/internal/signing"
	"github.com/you/orderguard/internal/todo"
)

const (
	adminEmail    = "admin@local.test"
	adminPassword = "change-me-please"
	syncRows      = 50
	password      = "correct-horse-battery"
)

var secret = []byte("e2e-secret-0123456789")

type registry struct{}

func (registry) Lookup(_ context.Context, cnpj string) (company.Company, error) {
	return company.Company{CNPJ: cnpj, RazaoSocial: "Empresa " + cnpj, CNAEsSecundarios: []company.CNAE{}}, nil
}

// startPocketBase runs PocketBase on a free port and returns its URL.
func startPocketBase(t *testing.T) string {
	t.Helper()
	exe := "pocketbase"
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin", exe)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("no PocketBase binary at %s; run node scripts/run.mjs once", bin)
	}
	flags := []string{
		"--dir=" + filepath.Join(t.TempDir(), "pb_data"),
		"--migrationsDir=" + filepath.Join(root, "pocketbase", "pb_migrations"),
		"--hooksDir=" + filepath.Join(root, "pocketbase", "pb_hooks"),
	}
	if out, err := exec.Command(bin, append([]string{"superuser", "upsert", adminEmail, adminPassword}, flags...)...).CombinedOutput(); err != nil {
		t.Fatalf("superuser: %v\n%s", err, out)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()

	cmd := exec.Command(bin, append([]string{"serve", "--http=" + addr}, flags...)...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })

	base := "http://" + addr
	for range 100 {
		if res, err := http.Get(base + "/api/health"); err == nil {
			res.Body.Close()
			return base
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("PocketBase never came up")
	return ""
}

// start assembles the stack the way the two mains do, and returns the gateway
// and the order store.
func start(t *testing.T) (*httptest.Server, *pocketbase.OrderRepository) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	pb := pocketbase.New(startPocketBase(t), adminEmail, adminPassword)
	orders := pocketbase.NewOrderRepository(pb)
	importer := orderimport.NewImporter(orders, 1, 100)
	queue, err := orderimport.NewQueue(pocketbase.NewImportJobs(pb), importer, t.TempDir(), log)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); queue.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })

	upstream := httptest.NewServer(api.New(api.Deps{
		Accounts:       auth.NewService(pocketbase.NewAccounts(pb), pocketbase.NewCompanyRepository(pb), registry{}, log),
		Orders:         order.NewService(orders, order.NoSearch{}),
		Imports:        orderimport.NewService(importer, queue, syncRows),
		Todos:          todo.NewService(pocketbase.NewTodoRepository(pb)),
		MaxImportBytes: 8 << 20,
		Log:            log,
	}))
	t.Cleanup(upstream.Close)
	target, _ := url.Parse(upstream.URL)

	srv := httptest.NewServer(gateway.New(gateway.Deps{
		Verifier:    signing.Verifier{Secret: secret, MaxAge: time.Minute, Skew: 5 * time.Second},
		Limits:      gateway.Limits{Body: 1 << 20, Import: 8 << 20},
		Idempotency: pocketbase.NewIdempotencyStore(pb),
		Sessions:    pocketbase.NewAccounts(pb),
		Limiter:     ratelimit.New(0, 0),
		Upstream:    target,
		Log:         log,
	}))
	t.Cleanup(srv.Close)
	return srv, orders
}

type client struct {
	t     *testing.T
	base  string
	token string
	seq   int
}

// do sends a signed request and decodes the JSON answer into out, if it isn't
// nil. An empty key gets a fresh one.
func (c *client) do(method, uri, key string, body []byte, out any) int {
	c.t.Helper()
	if key == "" {
		c.seq++
		key = fmt.Sprintf("e2e-key-%d-%d", time.Now().UnixNano(), c.seq)
	}
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	req, err := http.NewRequest(method, c.base+uri, bytes.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set(signing.HeaderKey, key)
	req.Header.Set(signing.HeaderTimestamp, ts)
	req.Header.Set(signing.HeaderSignature, signing.Sign(secret, method, uri, ts, key, body))
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			c.t.Fatalf("%s %s: %d %s", method, uri, res.StatusCode, raw)
		}
	}
	return res.StatusCode
}

func (c *client) json(method, uri, key string, in, out any) int {
	c.t.Helper()
	body, err := json.Marshal(in)
	if err != nil {
		c.t.Fatal(err)
	}
	return c.do(method, uri, key, body, out)
}

func register(t *testing.T, srv *httptest.Server, email, cnpj string) *client {
	t.Helper()
	c := &client{t: t, base: srv.URL}
	var session struct {
		Token string `json:"token"`
	}
	status := c.json(http.MethodPost, "/auth/register", "", map[string]any{
		"name": "E2E", "email": email, "password": password, "cnpj": cnpj,
	}, &session)
	if status != http.StatusCreated {
		t.Fatalf("register: %d", status)
	}
	c.token = session.Token
	return c
}

type orderPage struct {
	Orders     []order.Order `json:"orders"`
	NextCursor string        `json:"next_cursor"`
}

func csv(rows int) []byte {
	var b strings.Builder
	b.WriteString("item,quantity\n")
	for i := range rows {
		fmt.Fprintf(&b, "item-%d,2\n", i)
	}
	return []byte(b.String())
}

func TestStack(t *testing.T) {
	srv, orders := start(t)
	ana := register(t, srv, "ana@example.com", "45334465000117")
	bia := register(t, srv, "bia@example.com", "11222333000181")

	t.Run("unsigned requests never reach the API", func(t *testing.T) {
		res, err := http.Post(srv.URL+"/me/orders", "application/json", strings.NewReader(`{"item":"x","quantity":1}`))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d", res.StatusCode)
		}
	})

	t.Run("a session is required", func(t *testing.T) {
		anon := &client{t: t, base: srv.URL}
		if status := anon.do(http.MethodGet, "/me", "", nil, nil); status != http.StatusUnauthorized {
			t.Fatalf("status = %d", status)
		}
	})

	t.Run("a repeated order is written once", func(t *testing.T) {
		in := map[string]any{"item": "Teclado", "quantity": 3}
		for range 3 {
			if status := ana.json(http.MethodPost, "/me/orders", "same-key-0001", in, nil); status != http.StatusCreated {
				t.Fatalf("status = %d", status)
			}
		}
		var page orderPage
		ana.do(http.MethodGet, "/me/orders", "", nil, &page)
		if len(page.Orders) != 1 {
			t.Fatalf("orders = %+v", page.Orders)
		}
	})

	t.Run("one user's orders stay theirs", func(t *testing.T) {
		var page orderPage
		bia.do(http.MethodGet, "/me/orders", "", nil, &page)
		if len(page.Orders) != 0 {
			t.Fatalf("orders = %+v", page.Orders)
		}
	})

	t.Run("a small file imports at once and only once", func(t *testing.T) {
		var first, second orderimport.Summary
		if status := bia.do(http.MethodPost, "/me/orders/import", "", csv(30), &first); status != http.StatusOK {
			t.Fatalf("status = %d", status)
		}
		bia.do(http.MethodPost, "/me/orders/import", "", csv(30), &second)
		if first.Created != 30 || second.Created != 0 || second.Duplicates != 30 {
			t.Fatalf("first = %+v, second = %+v", first, second)
		}
	})

	t.Run("a large file goes through the queue", func(t *testing.T) {
		var job struct {
			ID        string `json:"id"`
			Status    string `json:"status"`
			Created   int    `json:"created"`
			Processed int    `json:"processed"`
		}
		if status := ana.do(http.MethodPost, "/me/orders/import", "", csv(250), &job); status != http.StatusAccepted {
			t.Fatalf("status = %d", status)
		}
		uri := "/me/orders/import/" + job.ID
		for deadline := time.Now().Add(30 * time.Second); job.Status != orderimport.StatusDone; {
			if time.Now().After(deadline) || job.Status == orderimport.StatusFailed {
				t.Fatalf("job = %+v", job)
			}
			time.Sleep(50 * time.Millisecond)
			ana.do(http.MethodGet, uri, "", nil, &job)
		}
		if job.Created != 250 || job.Processed != 250 {
			t.Fatalf("job = %+v", job)
		}
		if status := bia.do(http.MethodGet, uri, "", nil, nil); status != http.StatusNotFound {
			t.Fatalf("another user's job: %d", status)
		}
	})

	t.Run("pages walk every order once", func(t *testing.T) {
		seen := map[string]bool{}
		uri := "/me/orders?limit=40"
		for {
			var page orderPage
			if status := ana.do(http.MethodGet, uri, "", nil, &page); status != http.StatusOK {
				t.Fatalf("status = %d", status)
			}
			for _, o := range page.Orders {
				if seen[o.ID] {
					t.Fatalf("saw %s twice", o.ID)
				}
				seen[o.ID] = true
			}
			if page.NextCursor == "" {
				break
			}
			uri = "/me/orders?limit=40&cursor=" + url.QueryEscape(page.NextCursor)
		}
		if len(seen) != 251 {
			t.Fatalf("orders = %d", len(seen))
		}
	})

	t.Run("the search indexer reads every order once, oldest first", func(t *testing.T) {
		seen := map[string]bool{}
		var after *order.Cursor
		for {
			batch, err := orders.ListSince(context.Background(), after, 70)
			if err != nil {
				t.Fatal(err)
			}
			if len(batch) == 0 {
				break
			}
			for _, o := range batch {
				if seen[o.ID] {
					t.Fatalf("saw %s twice", o.ID)
				}
				if after != nil && o.Created < after.Created {
					t.Fatalf("%s came out of order", o.ID)
				}
				seen[o.ID] = true
			}
			last := batch[len(batch)-1]
			after = &order.Cursor{Created: last.Created, ID: last.ID}
		}
		if len(seen) != 281 { // ana's 251 and bia's 30
			t.Fatalf("orders = %d", len(seen))
		}
	})

	t.Run("search answers 503 until it is configured", func(t *testing.T) {
		if status := ana.do(http.MethodGet, "/me/orders/search?q=item", "", nil, nil); status != http.StatusServiceUnavailable {
			t.Fatalf("status = %d", status)
		}
	})

	t.Run("totals come from the database", func(t *testing.T) {
		var totals order.Totals
		ana.do(http.MethodGet, "/me/orders/summary", "", nil, &totals)
		if totals.Orders != 251 || totals.Units != 503 || totals.Items != 251 || len(totals.TopItems) != 5 {
			t.Fatalf("totals = %+v", totals)
		}
		if totals.TopItems[0].Item != "Teclado" {
			t.Fatalf("top items = %+v", totals.TopItems)
		}
	})

	t.Run("to-dos round trip", func(t *testing.T) {
		var created todo.Todo
		if status := ana.json(http.MethodPost, "/me/todos", "", map[string]string{"title": "conferir"}, &created); status != http.StatusCreated {
			t.Fatalf("status = %d", status)
		}
		if status := bia.json(http.MethodPatch, "/me/todos/"+created.ID, "", map[string]bool{"done": true}, nil); status != http.StatusNotFound {
			t.Fatalf("another user's to-do: %d", status)
		}
		if status := ana.do(http.MethodDelete, "/me/todos/"+created.ID, "", nil, nil); status != http.StatusNoContent {
			t.Fatalf("delete: %d", status)
		}
	})
}

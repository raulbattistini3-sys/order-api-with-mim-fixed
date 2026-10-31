# orderguard

One click should mean one order, even with a man in the middle.

A Go order API on PocketBase, a Go gateway that protects it, and an attacker written as axios interceptors that proves the difference:

```
direct to the API  → 1 click, 14 orders (one of them with quantity × 100)
through the gateway → 1 click, 1 order
```

## Run it

You need Go 1.24+ and Node 20+. The same command works in Git Bash, PowerShell, cmd and sh:

```sh
node scripts/run.mjs               # exits 0 only if the gateway let exactly 1 order through
node scripts/run.mjs --serve       # keeps pocketbase :8090, api :8080, gateway :8081 up until Ctrl+C
```

On first run it downloads the PocketBase version pinned in `pocketbase/Dockerfile` into `bin/` and installs the attacker's dependencies. Service logs go to `bin/logs/`.

With Docker instead:

```sh
docker compose up --build          # pocketbase :8090, api :8080, gateway :8081
cd attacker && npm install
npm run demo
```

Tests: `go test ./...` and `cd attacker && npm test`. The two suites share a signature fixture, so the Go and JS implementations can't drift apart.

## What happens

```
victim (axios, signs)
   │
   ├─ tap interceptor ─▶ copies ─▶ attack interceptors ─┐
   │                                                    ▼
   └───────────────────────────────────────▶ api :8080, or gateway :8081 ─▶ api :8080 ─▶ pocketbase
```

An axios request interceptor runs after the client has decided what to send and before the request hits the wire, which is exactly where a man in the middle sits. A tap on the victim's client copies each signed request, and each attack is one interceptor applied to those copies. The attacker never has `CLIENT_SECRET`.

| attack | interceptor does | direct to the API | through the gateway |
|---|---|---|---|
| duplicate-race | nothing; 10 copies fired at once | 10 new orders | 0 new: one wins, the rest get `409` or a cached `201` (`Idempotent-Replayed: true`) |
| key-swap | sets a fresh `Idempotency-Key` | new order | `401` signature mismatch |
| body-tamper | `quantity *= 100` | order for 100 | `401` signature mismatch |
| late-replay | `await sleep(6s)`, then sends | new order | `401` request expired |

Sometimes a copy wins the race against the victim's request. That's fine: it's the same signed request, so it creates the victim's order once. The victim then gets `409`, retries with the same key, and receives the cached response.

## The fix, in two layers

Both run in the gateway, signature first.

**1. Signing (`internal/signing`)** handles requests that were rewritten. Idempotency alone can't stop an attacker who simply mints a new key. So the client signs:

```
METHOD \n REQUEST-URI \n TIMESTAMP_MS \n IDEMPOTENCY-KEY \n hex(sha256(body))
```

with HMAC-SHA256. Here's how each tampering attempt is covered:

- **Changing the key, body or path:** the signature breaks.
- **Exact replay inside the window:** absorbed by idempotency.
- **Exact replay after the window** (`SIGNATURE_MAX_AGE`, 30s by default): rejected as stale.

Keep idempotency records for at least as long as the signature window. Records here are never deleted, so that already holds.

**2. Idempotency (`internal/idempotency`)** handles duplicates, including duplicates spread across gateway replicas.

- The lock is a unique index on `idempotency_keys.key` in PocketBase. Concurrent requests race to insert a `pending` row, and exactly one wins. Because the lock lives in the database, it holds across any number of instances.
- The key is fingerprinted as `sha256(method, uri, body)`. Reusing a key with a different body returns `422`.
- A request whose key is still in flight gets `409` with `Retry-After`. Once the original finishes, the stored response is replayed.
- A `5xx` frees the key so the client can retry. A `pending` row older than 30s is treated as abandoned (its replica crashed) and can be taken over.
- Belt and braces: the gateway forwards the verified key as `X-Order-Ref`, and `orders.idempotency_key` has its own unique index. Even if the bookkeeping fails halfway, a second order with that key can't be written.

> Signing proves *integrity*. It does not give you *confidentiality*: a MITM can still read the traffic. In production you still want TLS. The API's port must also be private, so that the gateway is the only way in; it is published here only so the demo can show what happens without it.

## Layout

This follows the Go team's [server project layout](https://go.dev/doc/modules/layout#server-project): commands go in `cmd/`, everything else in `internal/`. It uses only the standard library.

Dependencies are passed in through constructors and wired in each `main`. Domain packages define the interface they need (`order.Repository`, `idempotency.Store`), and `internal/pocketbase` implements them, so nothing but `pocketbase` knows about the database.

```
cmd/api               order API: wiring only
cmd/gateway           protective gateway: wiring only
internal/order        Order, Repository, HTTP handlers
internal/gateway      signature → idempotency → reverse proxy
internal/signing      HMAC verify + middleware
internal/idempotency  Store interface + middleware
internal/pocketbase   REST client + the two repositories
internal/server       run loop, request logging, env
internal/httpjson     JSON response helpers
pocketbase/           Dockerfile + migrations (collections, unique indexes)
attacker/             victim client, attacks (axios interceptors), demo
scripts/run.mjs       runs everything without Docker
```

Rename the module path `github.com/you/orderguard` in `go.mod` to your own repo.

## API

Served by the gateway on `:8081`:

| | |
|---|---|
| `POST /orders` | Needs `Idempotency-Key` (8–255 chars of `[A-Za-z0-9_.:-]`), `X-Timestamp` (unix ms) and `X-Signature`. Body: `{"customer","item","quantity"}` |
| `GET /orders?customer=X` | `{count, orders}` with the 50 newest orders |
| `GET /healthz` | `204` |

The API on `:8080` has the same routes with no checks.

## Next: Next.js + load test

- **SSR keeps the secret out of the browser, but it isn't enough alone.** Server-side rendering only produces the page; the click happens later, in the browser. Send it to a Server Action or route handler, which signs on the server and calls the gateway. `CLIENT_SECRET` must never get a `NEXT_PUBLIC_` prefix. `attacker/src/client.js` is the shape to port: one key per user action, and the same key reused on every `409` retry.
- **Use SSR to issue the key.** Generate the idempotency key while rendering the form, so a double click or a retried submit carries the same key.
- **The browser → Next.js hop is outside the signature.** The server signs whatever arrives, so that hop needs TLS and a logged-in session. Signing protects from Next.js to the gateway onward.
- For the load test (k6 or autocannon), fire N clicks, each with its own key and many duplicates per key, against both `:8080` and `:8081`. Then compare `GET /orders?customer=…` counts. Expected result: `count == N` through the gateway and `count > N` direct.

package signing

import (
	"errors"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestVerify(t *testing.T) {
	secret := []byte("s3cret")
	now := time.UnixMilli(1_700_000_000_000)
	v := Verifier{Secret: secret, MaxAge: 30 * time.Second, Skew: 5 * time.Second, Now: func() time.Time { return now }}
	body := []byte(`{"customer":"c1","item":"keyboard","quantity":1}`)

	signed := func(ts time.Time, key string, b []byte) *httptestReq {
		stamp := strconv.FormatInt(ts.UnixMilli(), 10)
		return &httptestReq{key: key, ts: stamp, sig: Sign(secret, "POST", "/orders", stamp, key, b)}
	}

	tests := []struct {
		name string
		req  *httptestReq
		body []byte
		want error
	}{
		{"valid", signed(now, "key-00000001", body), body, nil},
		{"tampered body", signed(now, "key-00000001", body), []byte(`{"customer":"c1","item":"keyboard","quantity":100}`), ErrMismatch},
		{"swapped key", signed(now, "key-00000001", body).withKey("key-00000002"), body, ErrMismatch},
		{"stale", signed(now.Add(-31*time.Second), "key-00000001", body), body, ErrStale},
		{"future", signed(now.Add(time.Minute), "key-00000001", body), body, ErrFuture},
		{"unsigned", &httptestReq{}, body, ErrMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/orders", strings.NewReader(string(tt.body)))
			tt.req.apply(r.Header.Set)
			if err := v.Verify(r, tt.body); !errors.Is(err, tt.want) {
				t.Fatalf("Verify() = %v, want %v", err, tt.want)
			}
		})
	}
}

// TestCanonicalMatchesJS pins the canonical format so the JavaScript client
// (attacker/src/signing.js) can't silently drift.
func TestCanonicalMatchesJS(t *testing.T) {
	got := Sign([]byte("dev-secret"), "POST", "/orders", "1700000000000", "key-00000001", []byte(`{"a":1}`))
	const want = "446e517c4c2a898a4cd71817b8392a7ba280813e1e39e8ba4559426ce12a7de7"
	if got != want {
		t.Fatalf("Sign() = %s, want %s (JS client would disagree)", got, want)
	}
}

type httptestReq struct{ key, ts, sig string }

func (h *httptestReq) withKey(k string) *httptestReq { h.key = k; return h }

func (h *httptestReq) apply(set func(string, string)) {
	if h.key != "" {
		set(HeaderKey, h.key)
	}
	if h.ts != "" {
		set(HeaderTimestamp, h.ts)
	}
	if h.sig != "" {
		set(HeaderSignature, h.sig)
	}
}

// Package signing verifies HMAC-signed requests.
//
// Idempotency keys alone don't survive a man-in-the-middle: an attacker who can
// rewrite a request can mint a fresh key (or change the body) and the server
// sees a brand-new request. So the client signs everything that matters:
//
//	METHOD \n REQUEST-URI \n TIMESTAMP \n IDEMPOTENCY-KEY \n hex(sha256(BODY))
//
// Changing the key, the body or the path breaks the signature. Replaying the
// exact bytes inside the freshness window is absorbed by idempotency (same key,
// cached response). Replaying after the window is rejected as stale.
package signing

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/you/orderguard/internal/httpjson"
)

// Header names shared with every client.
const (
	HeaderKey       = "Idempotency-Key"
	HeaderTimestamp = "X-Timestamp" // unix milliseconds
	HeaderSignature = "X-Signature" // hex HMAC-SHA256
)

// MaxBody caps how much of a request body is read for verification.
const MaxBody = 1 << 20

var (
	ErrMissing   = errors.New("missing signature headers")
	ErrTimestamp = errors.New("malformed timestamp")
	ErrStale     = errors.New("request expired")
	ErrFuture    = errors.New("timestamp is in the future")
	ErrMismatch  = errors.New("signature mismatch")
)

// Canonical is the exact string that gets signed.
func Canonical(method, requestURI, timestamp, key string, body []byte) string {
	sum := sha256.Sum256(body)
	return method + "\n" + requestURI + "\n" + timestamp + "\n" + key + "\n" + hex.EncodeToString(sum[:])
}

// Sign returns the hex HMAC-SHA256 of the canonical string.
func Sign(secret []byte, method, requestURI, timestamp, key string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(Canonical(method, requestURI, timestamp, key, body)))
	return hex.EncodeToString(mac.Sum(nil))
}

// Verifier checks signature and freshness.
type Verifier struct {
	Secret []byte
	MaxAge time.Duration    // how old a signed request may be
	Skew   time.Duration    // tolerated client clock drift into the future
	Now    func() time.Time // nil means time.Now
}

// Verify checks r's headers against body.
func (v Verifier) Verify(r *http.Request, body []byte) error {
	ts := r.Header.Get(HeaderTimestamp)
	sig := r.Header.Get(HeaderSignature)
	key := r.Header.Get(HeaderKey)
	if ts == "" || sig == "" || key == "" {
		return ErrMissing
	}
	ms, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return ErrTimestamp
	}
	now := time.Now
	if v.Now != nil {
		now = v.Now
	}
	age := now().Sub(time.UnixMilli(ms))
	if age > v.MaxAge {
		return ErrStale
	}
	if age < -v.Skew {
		return ErrFuture
	}
	want := Sign(v.Secret, r.Method, r.URL.RequestURI(), ts, key, body)
	if !hmac.Equal([]byte(want), []byte(sig)) {
		return ErrMismatch
	}
	return nil
}

// Middleware rejects unsigned, tampered or stale requests with 401. It leaves
// the body readable for the next handler.
func (v Verifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBody))
		if err != nil {
			httpjson.Error(w, http.StatusRequestEntityTooLarge, "body too large")
			return
		}
		if err := v.Verify(r, body); err != nil {
			httpjson.Error(w, http.StatusUnauthorized, err.Error())
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(w, r)
	})
}

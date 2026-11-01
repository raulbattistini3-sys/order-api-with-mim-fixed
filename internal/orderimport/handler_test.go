package orderimport

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/you/orderguard/internal/auth"
)

func TestImportRejectsFileOverLimit(t *testing.T) {
	mux := http.NewServeMux()
	Routes(mux, NewService(nil, nil, 1), 8, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodPost, "/me/orders/import", strings.NewReader("123456789"))
	req.Header.Set(auth.UserHeader, "u1")
	req.Header.Set(auth.CompanyHeader, "c1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", rec.Code)
	}
}

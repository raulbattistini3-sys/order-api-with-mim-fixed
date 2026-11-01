package orderimport

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/you/orderguard/internal/auth"
	"github.com/you/orderguard/internal/httpjson"
)

// Handler serves the spreadsheet import endpoint.
type Handler struct {
	importer *Importer
	maxBytes int64
	log      *slog.Logger
}

// NewHandler returns a Handler backed by importer. maxBytes caps the upload
// size.
func NewHandler(importer *Importer, maxBytes int64, log *slog.Logger) *Handler {
	return &Handler{importer: importer, maxBytes: maxBytes, log: log}
}

// Register adds the import route to mux. It expects the gateway's identity
// headers, so it goes behind auth.RequireIdentity.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("POST /me/orders/import", auth.RequireIdentity(http.HandlerFunc(h.importOrders)))
}

func (h *Handler) importOrders(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.maxBytes))
	if err != nil {
		httpjson.Error(w, http.StatusRequestEntityTooLarge, "file too large")
		return
	}
	rows, skipped, err := Parse(data)
	switch {
	case errors.Is(err, ErrTooManyRows):
		httpjson.Error(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	case err != nil:
		httpjson.Error(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	id, _ := auth.IdentityFrom(r.Context())
	// The batch id scopes the deterministic keys to this user and this file:
	// the same file re-imported writes each order once; another user's copy is
	// independent.
	sum := sha256.Sum256(data)
	batchID := id.UserID + ":" + hex.EncodeToString(sum[:])

	summary, err := h.importer.Import(r.Context(), id.UserID, id.CompanyID, batchID, rows, skipped)
	if err != nil {
		h.log.Error("import orders", "err", err)
		httpjson.Error(w, http.StatusInternalServerError, "import interrupted")
		return
	}
	httpjson.Write(w, http.StatusOK, summary)
}

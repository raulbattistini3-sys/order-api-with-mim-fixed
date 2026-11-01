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

// Handler serves the spreadsheet import endpoints.
type Handler struct {
	importer *Importer
	queue    *Queue
	syncRows int
	maxBytes int64
	log      *slog.Logger
}

// NewHandler returns a Handler. Files of up to syncRows rows are imported while
// the client waits, larger ones go to queue. maxBytes caps the upload size.
func NewHandler(importer *Importer, queue *Queue, syncRows int, maxBytes int64, log *slog.Logger) *Handler {
	return &Handler{importer: importer, queue: queue, syncRows: syncRows, maxBytes: maxBytes, log: log}
}

// Register adds the import routes to mux. They expect the gateway's identity
// headers, so they go behind auth.RequireIdentity.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("POST /me/orders/import", auth.RequireIdentity(http.HandlerFunc(h.importOrders)))
	mux.Handle("GET /me/orders/import/{id}", auth.RequireIdentity(http.HandlerFunc(h.jobStatus)))
}

// jobResponse is what a client sees of a Job.
type jobResponse struct {
	ID         string     `json:"id"`
	Status     string     `json:"status"`
	Total      int        `json:"total"`
	Processed  int        `json:"processed"`
	Created    int        `json:"created"`
	Duplicates int        `json:"duplicates"`
	Failed     int        `json:"failed"`
	Skipped    []RowError `json:"skipped"`
}

func newJobResponse(j Job) jobResponse {
	return jobResponse{
		ID: j.ID, Status: j.Status, Total: j.Total, Processed: j.Processed,
		Created: j.Imported, Duplicates: j.Duplicates, Failed: j.Failed, Skipped: j.Skipped,
	}
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

	if len(rows) > h.syncRows {
		job, err := h.queue.Enqueue(r.Context(), id.UserID, id.CompanyID, batchID, data, len(rows))
		if err != nil {
			h.log.Error("enqueue import", "err", err)
			httpjson.Error(w, http.StatusInternalServerError, "import not accepted")
			return
		}
		httpjson.Write(w, http.StatusAccepted, newJobResponse(job))
		return
	}

	summary, err := h.importer.Import(r.Context(), id.UserID, id.CompanyID, batchID, rows, skipped)
	if err != nil {
		h.log.Error("import orders", "err", err)
		httpjson.Error(w, http.StatusInternalServerError, "import interrupted")
		return
	}
	httpjson.Write(w, http.StatusOK, summary)
}

func (h *Handler) jobStatus(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.IdentityFrom(r.Context())
	job, err := h.queue.store.UserJob(r.Context(), id.UserID, r.PathValue("id"))
	switch {
	case errors.Is(err, ErrJobNotFound):
		httpjson.Error(w, http.StatusNotFound, "import not found")
	case err != nil:
		h.log.Error("import status", "err", err)
		httpjson.Error(w, http.StatusInternalServerError, "internal error")
	default:
		httpjson.Write(w, http.StatusOK, newJobResponse(job))
	}
}

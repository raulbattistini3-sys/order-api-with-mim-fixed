package orderimport

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/you/orderguard/internal/auth"
	"github.com/you/orderguard/internal/httpjson"
)

// Routes adds the import endpoints to mux. maxBytes caps the upload size.
func Routes(mux *http.ServeMux, svc Service, maxBytes int64, log *slog.Logger) {
	mux.Handle("POST /me/orders/import", auth.RequireIdentity(importOrders(svc, maxBytes, log)))
	mux.Handle("GET /me/orders/import/{id}", auth.RequireIdentity(jobStatus(svc, log)))
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

func importOrders(svc Service, maxBytes int64, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBytes))
		if err != nil {
			httpjson.Error(w, http.StatusRequestEntityTooLarge, "file too large")
			return
		}
		id, _ := auth.IdentityFrom(r.Context())
		summary, job, err := svc.Submit(r.Context(), id.UserID, id.CompanyID, data)
		var unreadable *ParseError
		switch {
		case errors.Is(err, ErrTooManyRows):
			httpjson.Error(w, http.StatusRequestEntityTooLarge, err.Error())
		case errors.As(err, &unreadable):
			httpjson.Error(w, http.StatusUnprocessableEntity, err.Error())
		case err != nil:
			log.Error("import orders", "err", err)
			httpjson.Error(w, http.StatusInternalServerError, "import interrupted")
		case job != nil:
			httpjson.Write(w, http.StatusAccepted, newJobResponse(*job))
		default:
			httpjson.Write(w, http.StatusOK, summary)
		}
	}
}

func jobStatus(svc Service, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, _ := auth.IdentityFrom(r.Context())
		job, err := svc.Job(r.Context(), id.UserID, r.PathValue("id"))
		switch {
		case errors.Is(err, ErrJobNotFound):
			httpjson.Error(w, http.StatusNotFound, "import not found")
		case err != nil:
			log.Error("import status", "err", err)
			httpjson.Error(w, http.StatusInternalServerError, "internal error")
		default:
			httpjson.Write(w, http.StatusOK, newJobResponse(job))
		}
	}
}

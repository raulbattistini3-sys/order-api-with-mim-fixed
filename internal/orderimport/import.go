package orderimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/you/orderguard/internal/order"
)

// Summary is the outcome of an import.
type Summary struct {
	Total      int        `json:"total"`      // valid rows attempted
	Created    int        `json:"created"`    // new orders written
	Duplicates int        `json:"duplicates"` // rows already imported before
	Failed     int        `json:"failed"`     // rows that errored on write
	Skipped    []RowError `json:"skipped"`    // rows that didn't parse
}

// Importer writes parsed rows as orders, with a bounded number of workers.
type Importer struct {
	repo    order.Repository
	workers int
}

// NewImporter returns an Importer. workers caps how many rows are written at
// once; the gateway's rate limit guards the front door, this guards the store.
func NewImporter(repo order.Repository, workers int) *Importer {
	if workers < 1 {
		workers = 1
	}
	return &Importer{repo: repo, workers: workers}
}

const (
	// maxSampledErrors bounds how many per-row failures are returned, so a
	// wholly broken file can't produce a huge response.
	maxSampledErrors = 50
	// reportEvery is how many finished rows pass between progress reports.
	reportEvery = 1000
)

// Import writes rows for one user. batchID makes the idempotency keys
// deterministic: the same file imported twice writes each order once.
func (im *Importer) Import(ctx context.Context, userID, companyID, batchID string, rows []Row, skipped []RowError) (Summary, error) {
	return im.stream(ctx, userID, companyID, batchID, rows, skipped, nil)
}

// stream is Import with progress: every reportEvery rows it offers report the
// counts so far, with Total meaning rows finished. A busy receiver misses one.
func (im *Importer) stream(ctx context.Context, userID, companyID, batchID string, rows []Row, skipped []RowError, report chan<- Summary) (Summary, error) {
	var finished, created, duplicates, failed atomic.Int64

	var mu sync.Mutex
	sampled := append([]RowError(nil), skipped...)

	write := func(row Row) {
		o := order.Order{
			Customer:       userID,
			Company:        companyID,
			Item:           row.Item,
			Quantity:       row.Quantity,
			IdempotencyKey: rowKey(batchID, row.Line),
		}
		switch _, err := im.repo.Create(ctx, o); {
		case errors.Is(err, order.ErrDuplicate):
			duplicates.Add(1)
		case err != nil:
			failed.Add(1)
			mu.Lock()
			if len(sampled) < maxSampledErrors+len(skipped) {
				sampled = append(sampled, RowError{Line: row.Line, Message: "could not be saved"})
			}
			mu.Unlock()
		default:
			created.Add(1)
		}
		if n := finished.Add(1); report != nil && n%reportEvery == 0 {
			select {
			case report <- Summary{Total: int(n), Created: int(created.Load()), Duplicates: int(duplicates.Load()), Failed: int(failed.Load())}:
			default:
			}
		}
	}

	// Rows are interchangeable, so a fixed pool of workers shares one buffered
	// channel. The buffer keeps workers busy while the feed below catches up.
	jobs := make(chan Row, im.workers*2)
	var wg sync.WaitGroup
	for range im.workers {
		wg.Go(func() {
			for row := range jobs {
				write(row)
			}
		})
	}

feed:
	for _, row := range rows {
		select {
		case jobs <- row:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return Summary{}, err
	}
	return Summary{
		Total:      len(rows),
		Created:    int(created.Load()),
		Duplicates: int(duplicates.Load()),
		Failed:     int(failed.Load()),
		Skipped:    sampled,
	}, nil
}

// rowKey is the per-row idempotency key: stable for a (batch, line) pair, and
// within the key charset the gateway and store accept.
func rowKey(batchID string, line int) string {
	sum := sha256.Sum256([]byte(batchID + ":" + strconv.Itoa(line)))
	return "imp_" + hex.EncodeToString(sum[:16])
}

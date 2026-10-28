package api

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The CSV exports (ADR 0020, decision 3): a GET at <list path>/export with the list's filters
// and permission, streamed row by row from the store with encoding/csv.

// defaultExportMaxRows caps an export; a larger one is a 413 export-too-large problem.
const defaultExportMaxRows = 100_000

// errExportTooLarge is an export with more rows than its cap, refused before any byte is written.
var errExportTooLarge = errors.New("export too large")

// csvExport is a streamed CSV answer: rows reads the store and hands each row to emit. Nothing
// is written until the first row (or the end of an empty export), so an error before it is a
// problem; one after it ends the stream early, since the status is sent.
type csvExport struct {
	ctx    context.Context //nolint:containedctx // the request's context, kept for the generated Visit call
	log    *slog.Logger
	what   string // the file name's first part: runs, spend
	day    time.Time
	header []string
	max    int
	rows   func(ctx context.Context, maxRows int, emit func([]string) error) error
}

func (e csvExport) VisitExportRunsResponse(w http.ResponseWriter) error  { return e.visit(w) }
func (e csvExport) VisitExportSpendResponse(w http.ResponseWriter) error { return e.visit(w) }

func (e csvExport) visit(w http.ResponseWriter) error {
	cw := csv.NewWriter(w)
	started := false

	start := func() error {
		started = true

		h := w.Header()
		h.Set("Content-Type", "text/csv; charset=utf-8")
		h.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s.csv"`, e.what, e.day.UTC().Format(time.DateOnly)))
		h.Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)

		return cw.Write(e.header)
	}

	err := e.rows(e.ctx, e.max, func(rec []string) error {
		if !started {
			if err := start(); err != nil {
				return err
			}
		}

		return cw.Write(rec)
	})

	switch {
	case errors.Is(err, ports.ErrTooMany):
		return fmt.Errorf("%w: more than %d rows; narrow the filters", errExportTooLarge, e.max)
	case err != nil && !started:
		return err
	case err != nil:
		e.log.WarnContext(e.ctx, "export ended early", slog.String("export", e.what), slog.Any("error", err))

		return nil
	case !started:
		_ = start() // the status is sent: a failed write means the client left
	}

	cw.Flush()

	return nil
}

// usd writes an amount in decimal USD, as short as it reads back exactly.
func usd(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// exportProblem is the answer to an export over its cap.
func exportProblem(err error) (gen.Problem, bool) {
	if !errors.Is(err, errExportTooLarge) {
		return gen.Problem{}, false
	}

	p := detailed(http.StatusRequestEntityTooLarge, err)
	p.Code = optional("export-too-large")

	return p, true
}

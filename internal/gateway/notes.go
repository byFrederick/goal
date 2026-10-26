package gateway

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// notesAfterParam is the query parameter of GET /run that asks for the run's notes instead of
// its bundle: the id of the last note event the worker has, 0 for none.
const notesAfterParam = "notes_after"

// maxNotes is how many notes one answer lists at most; the worker asks again after the last.
const maxNotes = 50

// runNote is one note a person left on the run's item while the run works.
type runNote struct {
	// ID is the note's event id, which the worker reports in note_delivered and passes as
	// notes_after next.
	ID int64 `json:"id"`
	// At is when the note was left, RFC 3339.
	At string `json:"at"`
	// By is the actor ref of the person who left it.
	By   string `json:"by"`
	Text string `json:"text"`
}

// runNotes is the body of GET /run?notes_after.
type runNotes struct {
	Notes []runNote `json:"notes"`
}

// serveNotes answers GET /run?notes_after=<id> for the run of claims: the note events of its
// item after both its own run_started and id, oldest first, at most maxNotes. A notes_after that
// is not 0 or a positive integer is 400; a token whose factory, version, agent, station or item
// the organization does not have is 403, as for the bundle. It records nothing.
func (g *Gateway) serveNotes(ctx context.Context, w http.ResponseWriter, r *http.Request, claims domain.TaskClaims) {
	after, ok := parseNotesAfter(r.URL.Query().Get(notesAfterParam))
	if !ok {
		http.Error(w, "notes_after must be 0 or a positive integer", http.StatusBadRequest)
		return
	}

	notes, err := g.notes(ctx, claims, after)
	switch {
	case errors.Is(err, errForbidden) || errors.Is(err, ports.ErrNotFound):
		g.log.WarnContext(ctx, "run notes forbidden", claimAttrs(claims, "reason", err)...)
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	case err != nil:
		g.log.ErrorContext(ctx, "run notes failed", claimAttrs(claims, "error", err)...)
		http.Error(w, "gateway unavailable", http.StatusServiceUnavailable)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	if err := json.NewEncoder(w).Encode(runNotes{Notes: notes}); err != nil {
		g.log.WarnContext(ctx, "write run notes", claimAttrs(claims, "error", err)...)
		return
	}

	g.log.DebugContext(ctx, "run notes served", claimAttrs(claims, "notes", len(notes), "after", after)...)
}

// parseNotesAfter reads notes_after: decimal digits only, so 0 or a positive integer.
func parseNotesAfter(s string) (int64, bool) {
	if s == "" || s[0] < '0' || s[0] > '9' {
		return 0, false
	}

	n, err := strconv.ParseInt(s, 10, 64)

	return n, err == nil
}

// notes are the note events of c's item after both c's run's run_started and the event id after,
// oldest first, at most maxNotes. Until the run's run_started is found none counts, so a note
// left before the run, or on a run that was never recorded, is never listed.
func (g *Gateway) notes(ctx context.Context, c domain.TaskClaims, after int64) ([]runNote, error) {
	if _, err := g.run(ctx, c); err != nil {
		return nil, err
	}

	notes := []runNote{}
	started := false

	for from := int64(0); ; {
		events, err := g.events.ItemEvents(ctx, c.ItemID, from, eventPage)
		if err != nil {
			return nil, fmt.Errorf("events of item %s: %w", c.ItemID, err)
		}

		for _, e := range events {
			switch {
			case !started:
				started = e.Kind == domain.EventRunStarted && e.RunID == c.RunID
			case e.Kind == domain.EventNote && e.ID > after:
				notes = append(notes, noteOf(e))
				if len(notes) == maxNotes {
					return notes, nil
				}
			}
		}

		if len(events) < eventPage {
			return notes, nil
		}

		from = events[len(events)-1].ID
	}
}

// noteOf is note event e as a run reads it.
func noteOf(e domain.Event) runNote {
	by, _ := e.Data["actor"].(string)
	text, _ := e.Data["text"].(string)

	return runNote{ID: e.ID, At: e.At.UTC().Format(time.RFC3339), By: cmp.Or(by, e.Actor), Text: text}
}

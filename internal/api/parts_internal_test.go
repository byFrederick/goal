package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

// An added part whose needs would close a cycle is 409 edge-cycle, though domain.ErrCycle comes
// with a rule broken: the store's refusals only reach the API through the engine, and one part
// added to an acyclic plan cannot make a cycle there.
func TestAPI_AddItemPartCycleIsEdgeCycle(t *testing.T) {
	t.Parallel()

	err := partRefusal(fmt.Errorf("add part: %w", fmt.Errorf("needs: %w (%w)", domain.ErrCycle, domain.ErrInvalid)))

	if p := problemOf(err); p.Status != http.StatusConflict || deref(p.Code) != "edge-cycle" {
		t.Errorf("a cycle = %d %q; want 409 edge-cycle", p.Status, deref(p.Code))
	}
}

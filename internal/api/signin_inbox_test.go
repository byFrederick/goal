package api_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

// Spec 3 §8.4: the inbox and the attention badges show only the factories the caller sees, and
// count a decision as theirs only where they may decide.
func TestAPI_SignIn_InboxAndAttentionByPermission(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)
	h.seedInboxItems(t) // line's decision, blocked, paused and expedite items

	if err := h.identity.GrantFactoryRole(testOrg, viewerID, lineID, domain.FactoryViewer); err != nil {
		t.Fatal(err)
	}

	kinds := func(token string) []gen.InboxEntryKind {
		var out []gen.InboxEntryKind
		for _, e := range decode[gen.Inbox](t, h.as(t, token, http.MethodGet, acmePath+"/inbox", ""), http.StatusOK).Entries {
			out = append(out, e.Kind)
		}

		return out
	}

	if got := kinds(ownerToken); !slices.Contains(got, gen.InboxEntryKindDecision) {
		t.Errorf("owner's inbox = %v, want line's decision", got)
	}

	if got := kinds(viewerToken); slices.Contains(got, gen.InboxEntryKindDecision) || !slices.Contains(got, gen.InboxEntryKindBlocked) {
		t.Errorf("viewer's inbox = %v, want line's blocked items and no decision", got)
	}

	onYou := func(token string) int {
		return decode[gen.OrgAttention](t, h.as(t, token, http.MethodGet, acmePath+"/attention", ""), http.StatusOK).WaitingOnYou
	}

	if owner, viewer := onYou(ownerToken), onYou(viewerToken); owner != 1 || viewer != 0 {
		t.Errorf("waiting on the owner %d, on the viewer %d; want 1 and 0", owner, viewer)
	}

	if err := h.identity.GrantFactoryRole(testOrg, viewerID, lineID, ""); err != nil {
		t.Fatal(err)
	}

	if got := kinds(viewerToken); len(got) != 0 {
		t.Errorf("inbox of a viewer of acme alone = %v, want nothing of line", got)
	}
}

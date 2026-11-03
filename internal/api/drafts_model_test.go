package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
)

// knowledgeSources is acme's knowledge.sources with a name, an entry with appliesTo and
// readWhen, and a name again, and the same list as the model's JSON.
const (
	knowledgeSources = `  knowledge:
    sources:
      - handbook
      - name: runbooks
        appliesTo: { agents: [writer], stations: [approve] }
        readWhen: Before you touch production.
      - wiki
`
	knowledgeSourcesJSON = `["handbook",{"appliesTo":{"agents":["writer"],"stations":["approve"]},"name":"runbooks",` +
		`"readWhen":"Before you touch production."},"wiki"]`
)

// acmeWithKnowledge is acme reading knowledgeSources.
var acmeWithKnowledge = strings.Replace(acme, "  agents:\n", knowledgeSources+"  agents:\n", 1)

// knowledgeSourcesOf is the draft model's basics.knowledge_sources as compact JSON.
func knowledgeSourcesOf(t *testing.T, m gen.DraftModel) string {
	t.Helper()

	var s struct {
		Basics struct {
			KnowledgeSources json.RawMessage `json:"knowledge_sources"`
		} `json:"basics"`
	}
	if err := json.Unmarshal(m.Model, &s); err != nil {
		t.Fatal(err)
	}

	return string(s.Basics.KnowledgeSources)
}

// The draft model keeps knowledge.sources entries that are mappings as factory.yaml writes them,
// and a basics save with names and mappings writes them back unchanged.
func TestAPI_DraftModelKnowledgeEntryObjects(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.openDraft(t)
	h.saveYAML(t, acmeWithKnowledge)

	m, s := h.model(t)
	if got := knowledgeSourcesOf(t, m); got != knowledgeSourcesJSON {
		t.Fatalf("knowledge_sources = %s, want %s", got, knowledgeSourcesJSON)
	}

	s.Basics["title"] = "Acme two"
	decode[gen.SaveResult](t, h.request(t, http.MethodPut, acmeDraft+"/model/basics", body(t, struct {
		Value map[string]any `json:"value"`
	}{s.Basics}),
		quoted(m.Etags.Basics)), http.StatusOK)

	saved, _ := h.model(t)
	if got := knowledgeSourcesOf(t, saved); got != knowledgeSourcesJSON {
		t.Errorf("knowledge_sources after the save = %s, want %s", got, knowledgeSourcesJSON)
	}

	file := h.request(t, http.MethodGet, acmeDraft+"/files/factory.yaml", "", nil)
	if !strings.Contains(string(file.body), knowledgeSources) {
		t.Errorf("factory.yaml after the save:\n%s\nwant knowledge.sources as written", file.body)
	}
}

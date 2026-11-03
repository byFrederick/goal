package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

// previewPath is acme's draft's Knowledge preview.
const previewPath = acmeDraft + "/knowledge-preview"

// previewYAML is acme with knowledgeSources, a designer who reads design only and a checker who
// may not read files.
var previewYAML = strings.Replace(acmeWithKnowledge, "      - wiki\n",
	"      - wiki\n      - name: design\n        appliesTo: { agents: [designer] }\n", 1)

// previewMemory is the draft's memory.yaml.
const previewMemory = `- text: The nightly vacuum clears staging-db disk alerts.
  from: [acme#12]
  added: 2026-11-01
`

// previewDraft is a harness whose acme draft reads previewYAML and previewMemory, with handbook
// (leave.md and db/disk.md) and runbooks (alerts/db/disk.md) synced, and no wiki or design.
func previewDraft(t *testing.T) *harness {
	t.Helper()

	h := newHarness(t)
	h.createHandbook(t)
	decode[gen.KnowledgeSource](t, h.request(t, http.MethodPost, sourcesPath, `{"name":"runbooks","title":"Runbooks","kind":"upload"}`, nil),
		http.StatusCreated)
	decode[gen.KnowledgeUploadList](t, h.uploadTo(t, handbookPath+"/uploads", [2]string{leaveFile, leaveMD}, [2]string{diskFile, diskMD}),
		http.StatusAccepted)
	decode[gen.KnowledgeUploadList](t, h.uploadTo(t, sourcesPath+"/runbooks/uploads", [2]string{"alerts/" + diskFile, diskMD}),
		http.StatusAccepted)
	h.syncAll(t)

	h.openDraft(t)
	h.saveYAML(t, strings.Replace(previewYAML, "    - { name: writer, prompt: ./agents/writer.md }\n",
		"    - { name: writer, prompt: ./agents/writer.md }\n    - { name: designer, prompt: ./agents/writer.md }\n"+
			"    - name: checker\n      prompt: ./agents/writer.md\n      builtins: { read: off }\n", 1))
	decode[gen.SaveResult](t, h.request(t, http.MethodPut, acmeDraft+"/files/memory.yaml", previewMemory,
		http.Header{http.CanonicalHeaderKey("if-none-match"): {"*"}}), http.StatusOK)

	return h
}

// The preview is the section a run of the agent gets: the sources that apply to it, each with
// its entry's readWhen or its snapshot's, its file count and top folders, a source with no
// snapshot named as not synced yet, and memory.yaml's facts.
func TestAPI_DraftKnowledgePreview(t *testing.T) {
	t.Parallel()

	h := previewDraft(t)

	at := decode[gen.KnowledgePreview](t, h.request(t, http.MethodGet, previewPath+"?agent=writer&station=approve", "", nil), http.StatusOK)
	for _, want := range []string{
		"## Knowledge",
		"- `/knowledge/handbook/`: ",
		"2 files; top folders: `db`.",
		"- `/knowledge/runbooks/`: Before you touch production. 1 file; top folders: `alerts`.",
		"Not synced yet, so left out: `wiki`.",
		"- The nightly vacuum clears staging-db disk alerts. (from acme#12)",
	} {
		if !strings.Contains(at.Section, want) {
			t.Errorf("section lacks %q:\n%s", want, at.Section)
		}
	}

	if strings.Contains(at.Section, "design") {
		t.Errorf("the writer's section names design, which applies to the designer only:\n%s", at.Section)
	}

	elsewhere := decode[gen.KnowledgePreview](t, h.request(t, http.MethodGet, previewPath+"?agent=writer", "", nil), http.StatusOK)
	if strings.Contains(elsewhere.Section, "runbooks") {
		t.Errorf("section outside approve names runbooks, which applies at approve only:\n%s", elsewhere.Section)
	}

	designer := decode[gen.KnowledgePreview](t, h.request(t, http.MethodGet, previewPath+"?agent=designer", "", nil), http.StatusOK)
	if !strings.Contains(designer.Section, "`design`") {
		t.Errorf("the designer's section does not name design:\n%s", designer.Section)
	}
}

// The preview refuses an agent the draft lacks and a factory.yaml that does not parse; a viewer
// may read it, and an agent that may not read files gets no section.
func TestAPI_DraftKnowledgePreviewRefusals(t *testing.T) {
	t.Parallel()

	h := previewDraft(t)

	refused(t, "an unknown agent", h.request(t, http.MethodGet, previewPath+"?agent=nobody", "", nil), http.StatusNotFound, "agent-not-found")

	if got := decode[gen.KnowledgePreview](t, h.request(t, http.MethodGet, previewPath+"?agent=checker", "", nil), http.StatusOK); got.Section != "" {
		t.Errorf("the checker's section = %q, want none", got.Section)
	}

	h.as(acmeMember(domain.FactoryViewer))

	if got := decode[gen.KnowledgePreview](t, h.request(t, http.MethodGet, previewPath+"?agent=writer", "", nil), http.StatusOK); got.Section == "" {
		t.Error("a viewer's preview is empty, want the writer's section")
	}

	h.as(acmeMember(domain.FactoryEditor))
	h.saveYAML(t, "spec: [")
	refused(t, "an unparsable factory.yaml", h.request(t, http.MethodGet, previewPath+"?agent=writer", "", nil), http.StatusConflict, "yaml-invalid")
}

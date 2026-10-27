package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Knowledge searches and reads the knowledge base of the organization in ctx, over the named
// sources only (*knowledge.Searcher): a document of another source is ports.ErrNotFound. It
// records which documents a run used and the verdicts its agent gives them (ADR 0027).
type Knowledge interface {
	Search(ctx context.Context, text string, sources []string, limit int) ([]domain.KnowledgeHit, error)
	Read(ctx context.Context, id domain.KnowledgeDocumentID, sources []string) (domain.KnowledgeDocument, string, error)
	// RecordSearch records a search use of each document among hits, for use's factory, item,
	// run and time.
	RecordSearch(ctx context.Context, use domain.KnowledgeUse, hits []domain.KnowledgeHit) error
	RecordUse(ctx context.Context, u domain.KnowledgeUse) error
	RunUses(ctx context.Context, run domain.RunID) ([]domain.KnowledgeUse, error)
	// Verdict records v on a document of sources and returns it as stored.
	Verdict(ctx context.Context, v domain.KnowledgeVerdict, sources []string) (domain.KnowledgeVerdict, error)
}

// Goal's own tools (spec 4 §5.9), served to every run whose factory's version lists knowledge
// sources: "knowledge__search", "knowledge__read" and "knowledge__verdict" (ADR 0027), in the
// <server>__<tool> form of connections' tools.
const (
	knowledgeServer = "knowledge"
	toolSearch      = "search"
	toolRead        = "read"
	toolVerdict     = "verdict"
)

// Limits of the knowledge tools.
const (
	defaultHits   = 8
	maxHits       = 20
	maxQueryRunes = 1000
	// maxReadBytes is the most of a document's text knowledge__read returns (200 KiB).
	maxReadBytes = 200 << 10
	// useTimeout bounds the recording of a call's knowledge uses, which never fails the call.
	useTimeout = 2 * time.Second
)

// The knowledge tools' input schemas.
const (
	searchSchema = `{"type":"object","additionalProperties":false,"required":["query"],"properties":{
"query":{"type":"string","minLength":1,"maxLength":1000,"description":"What to look for, in words"},
"limit":{"type":"integer","minimum":1,"maximum":20,"default":8,"description":"How many passages at most"},
"source":{"type":"string","description":"Only this one of the sources"}}}`
	readSchema = `{"type":"object","additionalProperties":false,"required":["document"],"properties":{
"document":{"type":"string","description":"A document id from knowledge__search"}}}`
	verdictSchema = `{"type":"object","additionalProperties":false,"required":["document","verdict"],"properties":{
"document":{"type":"string","description":"A document id this run searched or read"},
"verdict":{"type":"string","enum":["helped","misled"],"description":"Whether the document helped or misled you"},
"note":{"type":"string","maxLength":500,"description":"Why, in a sentence or two"}}}`
)

// knowledgeTools are Goal's tools as tools/list describes them, for a run that may search sources.
func knowledgeTools(sources []string) []*mcp.Tool {
	return []*mcp.Tool{
		{
			Name: knowledgeServer + sep + toolSearch,
			Description: "Search the organization's knowledge base (sources: " + strings.Join(sources, ", ") + ") by keywords and meaning. " +
				"Returns the best matching passages, best first, each with its document id, title, source and a link back when it has one.",
			InputSchema: json.RawMessage(searchSchema),
			Annotations: &mcp.ToolAnnotations{Title: "Search the knowledge base", ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(bool)},
		},
		{
			Name:        knowledgeServer + sep + toolRead,
			Description: "Read a whole document of the knowledge base by the id knowledge__search gave, up to 200 KiB of its text.",
			InputSchema: json.RawMessage(readSchema),
			Annotations: &mcp.ToolAnnotations{Title: "Read a knowledge document", ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(bool)},
		},
		{
			Name: knowledgeServer + sep + toolVerdict,
			Description: "Say whether a document this run searched or read helped or misled you, with an optional note of up to 500 characters. " +
				"A later verdict on the same document replaces this run's earlier one.",
			InputSchema: json.RawMessage(verdictSchema),
			// A verdict replaces the run's earlier one and deletes nothing.
			Annotations: &mcp.ToolAnnotations{Title: "Judge a knowledge document", IdempotentHint: true, DestructiveHint: new(bool), OpenWorldHint: new(bool)},
		},
	}
}

// searchable are the knowledge sources the runs of line may search through the gateway: none
// without a knowledge base.
func (g *Gateway) searchable(line domain.Line) []string {
	if g.knowledge == nil {
		return nil
	}

	return line.KnowledgeSources
}

// knowledgeTool reports whether name is one of Goal's tools this run is served, and which.
func (s *session) knowledgeTool(name string) (string, bool) {
	tool, ok := strings.CutPrefix(name, knowledgeServer+sep)

	return tool, ok && len(s.sources) > 0 && (tool == toolSearch || tool == toolRead || tool == toolVerdict)
}

// callKnowledge answers one call of Goal's tools and records it. Arguments the tool refuses and
// documents it may not read are tool errors the agent can act on; a document of a source the
// factory does not list answers as one that does not exist.
func (s *session) callKnowledge(ctx context.Context, tool string, args json.RawMessage) *mcp.CallToolResult {
	start := s.g.clock.Now()
	rec := call{connection: knowledgeServer, tool: tool, requestBytes: len(args)}

	var (
		text string
		err  error
	)

	switch tool {
	case toolSearch:
		text, err = s.search(ctx, args)
	case toolVerdict:
		text, err = s.verdict(ctx, args)
	default:
		text, err = s.read(ctx, args)
	}

	rec.outcome = outcomeOK

	var refusal *refusalError

	switch {
	case errors.As(err, &refusal):
		rec.outcome, rec.err, text = outcomeToolError, refusal.msg, refusal.msg
	case err != nil:
		rec.outcome, rec.err, text = outcomeFailed, err.Error(), "the knowledge base did not answer; try again"
		s.g.log.WarnContext(ctx, "gateway knowledge call failed", claimAttrs(s.claims, "tool", tool, "error", err)...)
	}

	rec.responseBytes = len(text)
	s.record(ctx, start, rec)

	return &mcp.CallToolResult{IsError: err != nil, Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// refusalError is a call the tool refuses, with the message the agent reads.
type refusalError struct{ msg string }

func (r *refusalError) Error() string { return r.msg }

func refuse(format string, a ...any) error { return &refusalError{msg: fmt.Sprintf(format, a...)} }

// search answers knowledge__search: the hits of the run's sources, or of the one it names among
// them, as JSON.
func (s *session) search(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Query  string `json:"query"`
		Limit  *int   `json:"limit"`
		Source string `json:"source"`
	}

	if err := json.Unmarshal(args, &in); err != nil {
		return "", refuse("the arguments are not {query, limit?, source?}: %v", err)
	}

	query := strings.TrimSpace(in.Query)
	if query == "" || utf8.RuneCountInString(query) > maxQueryRunes {
		return "", refuse("query is 1 to %d characters", maxQueryRunes)
	}

	limit := defaultHits
	if in.Limit != nil {
		limit = *in.Limit
	}

	if limit < 1 || limit > maxHits {
		return "", refuse("limit is 1 to %d", maxHits)
	}

	sources := s.sources
	if in.Source != "" {
		if !slices.Contains(s.sources, in.Source) {
			return "", refuse("source %q is not one this factory may search: %s", in.Source, strings.Join(s.sources, ", "))
		}

		sources = []string{in.Source}
	}

	hits, err := s.g.knowledge.Search(ctx, query, sources, limit)
	if err != nil {
		return "", fmt.Errorf("search: %w", err)
	}

	s.recordUse(ctx, toolSearch, func(ctx context.Context) error {
		return s.g.knowledge.RecordSearch(ctx, s.use(), hits)
	})

	type hit struct {
		Document string  `json:"document"`
		Source   string  `json:"source"`
		Title    string  `json:"title"`
		URL      string  `json:"url,omitempty"`
		Heading  string  `json:"heading,omitempty"`
		Snippet  string  `json:"snippet"`
		Score    float64 `json:"score"`
	}

	out := struct {
		Hits []hit `json:"hits"`
	}{Hits: make([]hit, 0, len(hits))}

	for _, h := range hits {
		out.Hits = append(out.Hits, hit{
			Document: string(h.Document), Source: h.Source, Title: h.Title, URL: h.URL, Heading: h.Heading, Snippet: h.Text, Score: h.Score,
		})
	}

	b, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("encode hits: %w", err)
	}

	return string(b), nil
}

// read answers knowledge__read: a document of the run's sources, its title and link first, then
// its text, cut at maxReadBytes.
func (s *session) read(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Document string `json:"document"`
	}

	if err := json.Unmarshal(args, &in); err != nil || in.Document == "" {
		return "", refuse("the arguments are {document}: a document id from knowledge__search")
	}

	d, text, err := s.g.knowledge.Read(ctx, domain.KnowledgeDocumentID(in.Document), s.sources)
	if errors.Is(err, ports.ErrNotFound) {
		return "", refuse("no document %q in the knowledge sources this factory may search", in.Document)
	}

	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}

	s.recordUse(ctx, toolRead, func(ctx context.Context) error {
		u := s.use()
		u.Source, u.Document, u.Kind = d.Source, d.ID, domain.KnowledgeRead

		return s.g.knowledge.RecordUse(ctx, u)
	})

	var b strings.Builder

	fmt.Fprintf(&b, "Title: %s\n", d.Title)

	if d.URL != "" {
		fmt.Fprintf(&b, "URL: %s\n", d.URL)
	}

	b.WriteString("\n")

	if len(text) > maxReadBytes {
		cut := maxReadBytes
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}

		fmt.Fprintf(&b, "%s\n\n[truncated: the document's text has %d bytes; this is the first %d]", text[:cut], len(text), cut)

		return b.String(), nil
	}

	b.WriteString(text)

	return b.String(), nil
}

// use is a knowledge use by this run, now; the caller names its document.
func (s *session) use() domain.KnowledgeUse {
	return domain.KnowledgeUse{Factory: s.claims.FactoryID, Item: s.claims.ItemID, Run: s.claims.RunID, At: s.g.clock.Now()}
}

// recordUse records a call's knowledge use, best effort (ADR 0027): the call has its answer, so a
// failure is logged with ids only, never a document's text, and the agent's cancellation does
// not drop it.
func (s *session) recordUse(ctx context.Context, tool string, record func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), useTimeout)
	defer cancel()

	if err := record(ctx); err != nil {
		s.g.log.WarnContext(ctx, "gateway knowledge use failed", claimAttrs(s.claims, "tool", tool, "error", err)...)
	}
}

// verdict answers knowledge__verdict: the agent's verdict on a document this run searched or
// read, of the run's sources, as stored, as JSON.
func (s *session) verdict(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Document string `json:"document"`
		Verdict  string `json:"verdict"`
		Note     string `json:"note"`
	}

	if err := json.Unmarshal(args, &in); err != nil || in.Document == "" {
		return "", refuse("the arguments are {document, verdict: helped|misled, note?}: a document id this run searched or read")
	}

	v := domain.KnowledgeVerdict{
		Document: domain.KnowledgeDocumentID(in.Document), Run: s.claims.RunID, Item: s.claims.ItemID,
		Verdict: domain.KnowledgeVerdictKind(in.Verdict), Note: strings.TrimSpace(in.Note),
		By: domain.Actor{Kind: domain.ActorAgent, ID: s.claims.Agent},
	}

	switch {
	case v.Verdict != domain.KnowledgeHelped && v.Verdict != domain.KnowledgeMisled:
		return "", refuse("verdict %q is not helped or misled", in.Verdict)
	case utf8.RuneCountInString(v.Note) > domain.MaxVerdictNote:
		return "", refuse("note has at most %d characters", domain.MaxVerdictNote)
	}

	if err := s.used(ctx, v.Document); err != nil {
		return "", err
	}

	got, err := s.g.knowledge.Verdict(ctx, v, s.sources)
	if errors.Is(err, ports.ErrNotFound) {
		return "", refuse("no document %q in the knowledge sources this factory may search", in.Document)
	}

	if err != nil {
		return "", fmt.Errorf("verdict: %w", err)
	}

	b, err := json.Marshal(struct {
		Document string    `json:"document"`
		Source   string    `json:"source"`
		Run      string    `json:"run"`
		Item     string    `json:"item"`
		Verdict  string    `json:"verdict"`
		Note     string    `json:"note,omitempty"`
		By       string    `json:"by"`
		At       time.Time `json:"at"`
	}{
		Document: string(got.Document), Source: string(got.Source), Run: string(got.Run), Item: string(got.Item),
		Verdict: string(got.Verdict), Note: got.Note, By: got.By.Ref(), At: got.At,
	})
	if err != nil {
		return "", fmt.Errorf("encode verdict: %w", err)
	}

	return string(b), nil
}

// used is nil when this run searched or read document; otherwise a refusal that says why: the
// document is not one of the run's sources, or the run has not used it.
func (s *session) used(ctx context.Context, document domain.KnowledgeDocumentID) error {
	uses, err := s.g.knowledge.RunUses(ctx, s.claims.RunID)
	if err != nil {
		return fmt.Errorf("run uses: %w", err)
	}

	if slices.ContainsFunc(uses, func(u domain.KnowledgeUse) bool { return u.Document == document }) {
		return nil
	}

	_, _, err = s.g.knowledge.Read(ctx, document, s.sources)

	switch {
	case errors.Is(err, ports.ErrNotFound):
		return refuse("no document %q in the knowledge sources this factory may search", document)
	case err != nil:
		return fmt.Errorf("read: %w", err)
	default:
		// A use that failed to record lands here too, so the refusal does not blame the agent.
		return refuse("this run has not searched or read document %q, or its use was not recorded: search or read it first", document)
	}
}

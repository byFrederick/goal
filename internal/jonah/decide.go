package jonah

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Authoring is the definition authoring a decision publishes and edits through (ADR 0034
// decisions 7 and 8), for the organization and as the actor of ctx. cmd/goald wires the API's
// adapter over authoring.Service.
type Authoring interface {
	// Head is the factory's current version (empty before the first publish) and whether it has
	// an open draft.
	Head(ctx context.Context, f domain.FactoryID) (current domain.DefinitionVersionID, draftOpen bool, err error)
	// CurrentFile is path's content in the current version; ok false when it has no such file.
	CurrentFile(ctx context.Context, f domain.FactoryID, path string) (content []byte, ok bool, err error)
	// VersionFile is path's content in the published version; ok false when it has no such
	// file.
	VersionFile(ctx context.Context, f domain.FactoryID, version domain.DefinitionVersionID, path string) (content []byte, ok bool, err error)
	// DraftFile is path's content in the open draft; ok false when it has no such file.
	DraftFile(ctx context.Context, f domain.FactoryID, path string) (content []byte, ok bool, err error)
	// OpenDraft opens a draft from the current version, or returns the open one with created
	// false, with its revision.
	OpenDraft(ctx context.Context, f domain.FactoryID) (revision int64, created bool, err error)
	// WriteFile writes path into the open draft, whatever is there, and returns the draft's
	// revision after it.
	WriteFile(ctx context.Context, f domain.FactoryID, path string, content []byte) (revision int64, err error)
	// Publish publishes the draft at revision while expected is the current version.
	Publish(ctx context.Context, f domain.FactoryID, revision int64, expected domain.DefinitionVersionID, message string) (domain.DefinitionVersion, error)
	// DiscardDraft discards the draft at revision.
	DiscardDraft(ctx context.Context, f domain.FactoryID, revision int64) error
}

// Proposals are the reads and writes of ports.JonahStore a decision makes.
type Proposals interface {
	Proposal(ctx context.Context, factory domain.FactoryID, number int64) (domain.Proposal, error)
	Proposals(ctx context.Context, q ports.ProposalQuery) ([]domain.Proposal, error)
	DecideProposal(ctx context.Context, factory domain.FactoryID, number int64, d domain.ProposalDecision) (domain.Proposal, error)
	UndoProposal(ctx context.Context, factory domain.FactoryID, undoes domain.ProposalID, version domain.DefinitionVersionID, n domain.NewProposal, at time.Time) (domain.Proposal, error)
	QueueJonahReview(ctx context.Context, n domain.NewJonahReview) (domain.JonahReview, error)
	LastDoneJonahReview(ctx context.Context, factory domain.FactoryID) (domain.JonahReview, error)
	JonahSpendSince(ctx context.Context, factory domain.FactoryID, since time.Time) (domain.JonahSpend, error)
}

// DecideDeps are the ports decisions work through. Log may be nil.
type DecideDeps struct {
	Proposals Proposals
	Authoring Authoring
	Audit     ports.Audit
	Clock     ports.Clock
	Log       *slog.Logger
}

// Decider decides proposals and queues manual reviews, as the person in ctx.
type Decider struct {
	d   DecideDeps
	log *slog.Logger
}

// Windows of a manual review and its budget.
const (
	// BudgetWindow is how far back Jonah's budget counts the line's and Jonah's runs (ADR 0039
	// decision 16).
	BudgetWindow = domain.JonahBudgetWindow
	// FirstWindow is how far back a factory's first review reads sent-back touches.
	FirstWindow = 30 * 24 * time.Hour
)

// NewDecider returns a Decider over d.
func NewDecider(d DecideDeps) *Decider {
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Decider{d: d, log: log}
}

// Approve publishes proposal number of f as a new version (decision 7): it applies the
// proposal's edits again to the current version's files (ADR 0039 decision 6), opens a draft
// from the current version, writes the results and publishes it. It refuses with
// domain.ErrProposalDraftOpen while a draft is open, domain.ErrProposalStale when an edit no
// longer applies (a replacement's old text no longer occurs exactly once), and
// ports.ErrProposalDecided once the proposal is decided; a publish refusal is returned as
// authoring gave it, with the draft discarded unless the publish keeps it.
func (dc *Decider) Approve(ctx context.Context, f domain.FactoryID, number int64) (domain.Proposal, domain.DefinitionVersion, error) {
	p, err := dc.proposed(ctx, f, number)
	if err != nil {
		return domain.Proposal{}, domain.DefinitionVersion{}, err
	}

	current, draftOpen, err := dc.d.Authoring.Head(ctx, f)
	if err != nil {
		return domain.Proposal{}, domain.DefinitionVersion{}, fmt.Errorf("approve proposal %d: %w", number, err)
	}

	if draftOpen {
		return domain.Proposal{}, domain.DefinitionVersion{}, fmt.Errorf("approve proposal %d: %w", number, domain.ErrProposalDraftOpen)
	}

	if current == "" {
		return domain.Proposal{}, domain.DefinitionVersion{}, fmt.Errorf("approve proposal %d: no current version: %w", number, domain.ErrProposalStale)
	}

	files, err := applyEdits(ctx, p, dc.d.Authoring.CurrentFile, f, domain.ErrProposalStale)
	if err != nil {
		return domain.Proposal{}, domain.DefinitionVersion{}, fmt.Errorf("approve proposal %d: %w", number, err)
	}

	revision, created, err := dc.d.Authoring.OpenDraft(ctx, f)
	if err != nil {
		return domain.Proposal{}, domain.DefinitionVersion{}, fmt.Errorf("approve proposal %d: open draft: %w", number, err)
	}

	if !created { // another writer opened one since Head
		return domain.Proposal{}, domain.DefinitionVersion{}, fmt.Errorf("approve proposal %d: %w", number, domain.ErrProposalDraftOpen)
	}

	v, err := dc.publish(ctx, f, p, files, revision, current)
	if err != nil {
		return domain.Proposal{}, domain.DefinitionVersion{}, err
	}

	decided, err := dc.decide(ctx, f, number, domain.ProposalDecision{State: domain.ProposalPublished, PublishedVersionID: v.ID})
	if err != nil {
		// The version is published, but another decision came first: say so, as the version
		// stays published and Restore is the undo.
		dc.log.WarnContext(ctx, "jonah proposal published but not recorded", slog.String("factory", string(f)),
			slog.Int64("proposal", number), slog.Int("version", v.Number), slog.Any("error", err))

		return domain.Proposal{}, domain.DefinitionVersion{}, err
	}

	if err := dc.audit(ctx, decided, domain.AuditProposalPublished, map[string]any{"version": v.Number}); err != nil {
		return domain.Proposal{}, domain.DefinitionVersion{}, err
	}

	return decided, v, nil
}

// publish writes files, p's edited files, into the draft at revision and publishes it,
// discarding the draft after a refusal.
func (dc *Decider) publish(ctx context.Context, f domain.FactoryID, p domain.Proposal, files []editedFile, revision int64,
	expected domain.DefinitionVersionID,
) (domain.DefinitionVersion, error) {
	v, err := func() (domain.DefinitionVersion, error) {
		for _, file := range files {
			r, err := dc.d.Authoring.WriteFile(ctx, f, file.Path, []byte(file.Content))
			if err != nil {
				return domain.DefinitionVersion{}, fmt.Errorf("write %s: %w", file.Path, err)
			}

			revision = r
		}

		return dc.d.Authoring.Publish(ctx, f, revision, expected, "Proposal "+strconv.FormatInt(p.Number, 10)+": "+p.Title)
	}()
	if err == nil {
		return v, nil
	}

	if derr := dc.d.Authoring.DiscardDraft(ctx, f, revision); derr != nil {
		dc.log.WarnContext(ctx, "jonah proposal draft not discarded", slog.String("factory", string(f)),
			slog.Int64("proposal", p.Number), slog.Any("error", derr))
	}

	return domain.DefinitionVersion{}, fmt.Errorf("approve proposal %d: %w", p.Number, err)
}

// Edit applies proposal number's edits to f's draft, opening one from the current version when
// none is open, and sets it applied (decision 8). It refuses with domain.ErrProposalDraftConflict,
// writing nothing, when an edit no longer applies to the draft's copy of its file, and with
// ports.ErrProposalDecided once the proposal is decided. It returns the draft's revision.
func (dc *Decider) Edit(ctx context.Context, f domain.FactoryID, number int64) (domain.Proposal, int64, error) {
	p, err := dc.proposed(ctx, f, number)
	if err != nil {
		return domain.Proposal{}, 0, err
	}

	revision, created, err := dc.d.Authoring.OpenDraft(ctx, f)
	if err != nil {
		return domain.Proposal{}, 0, fmt.Errorf("edit proposal %d: open draft: %w", number, err)
	}

	files, err := applyEdits(ctx, p, dc.d.Authoring.DraftFile, f, domain.ErrProposalDraftConflict)
	if err != nil {
		dc.discardOpened(ctx, f, number, revision, created)

		return domain.Proposal{}, 0, fmt.Errorf("edit proposal %d: %w", number, err)
	}

	for _, file := range files {
		if revision, err = dc.d.Authoring.WriteFile(ctx, f, file.Path, []byte(file.Content)); err != nil {
			return domain.Proposal{}, 0, fmt.Errorf("edit proposal %d: write %s: %w", number, file.Path, err)
		}
	}

	decided, err := dc.decide(ctx, f, number, domain.ProposalDecision{State: domain.ProposalApplied})
	if err != nil {
		return domain.Proposal{}, 0, err
	}

	if err := dc.audit(ctx, decided, domain.AuditProposalApplied, map[string]any{"draft_revision": revision}); err != nil {
		return domain.Proposal{}, 0, err
	}

	return decided, revision, nil
}

// discardOpened discards the draft at revision when this call opened it.
func (dc *Decider) discardOpened(ctx context.Context, f domain.FactoryID, number, revision int64, created bool) {
	if !created {
		return
	}

	if err := dc.d.Authoring.DiscardDraft(ctx, f, revision); err != nil {
		dc.log.WarnContext(ctx, "jonah proposal draft not discarded", slog.String("factory", string(f)),
			slog.Int64("proposal", number), slog.Any("error", err))
	}
}

// Reject sets proposal number rejected with reason, which later reviews read (decision 9). A
// blank reason is domain.ErrInvalid; ports.ErrProposalDecided once the proposal is decided.
func (dc *Decider) Reject(ctx context.Context, f domain.FactoryID, number int64, reason string) (domain.Proposal, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return domain.Proposal{}, fmt.Errorf("reject proposal %d: a rejection needs a reason: %w", number, domain.ErrInvalid)
	}

	decided, err := dc.decide(ctx, f, number, domain.ProposalDecision{State: domain.ProposalRejected, RejectReason: reason})
	if err != nil {
		return domain.Proposal{}, err
	}

	if err := dc.audit(ctx, decided, domain.AuditProposalRejected, map[string]any{"reason": reason}); err != nil {
		return domain.Proposal{}, err
	}

	return decided, nil
}

// maxUndoTitle is a proposal's longest title, which an undo's "Undo proposal N: " prefix may cut.
const maxUndoTitle = 200

// Undo writes Undo this change for proposal number of f (ADR 0039 decision 15): a new proposal,
// made against the current version, whose edits are the published change's replacements
// reversed. A whole file stored before replacements gives its replacements from the file it was
// made against and its content. The reversed replacements must apply to each file now, so the
// undo works whatever has published since. A file the change added
// is left: the reversed edits stop referring to it. Nothing publishes: the undo waits for a
// person like any proposal.
//
// It refuses with domain.ErrProposalNotPublished for a proposal never published,
// domain.ErrUndoConflict, naming the file, when a reversed edit's text no longer occurs exactly
// once, and ports.ErrConflict while an undo of the proposal is proposed, published or applied.
func (dc *Decider) Undo(ctx context.Context, f domain.FactoryID, number int64) (domain.Proposal, error) {
	by, err := actor(ctx)
	if err != nil {
		return domain.Proposal{}, err
	}

	p, err := dc.d.Proposals.Proposal(ctx, f, number)
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("read proposal %d: %w", number, err)
	}

	if p.State != domain.ProposalPublished || p.PublishedVersionID == "" {
		return domain.Proposal{}, fmt.Errorf("undo proposal %d, %s: %w", number, p.State, domain.ErrProposalNotPublished)
	}

	current, _, err := dc.d.Authoring.Head(ctx, f)
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("undo proposal %d: %w", number, err)
	}

	edits, err := dc.reversed(ctx, f, p)
	if err != nil {
		return domain.Proposal{}, err
	}

	title := "Undo proposal " + strconv.FormatInt(p.Number, 10) + ": " + p.Title
	if r := []rune(title); len(r) > maxUndoTitle {
		title = string(r[:maxUndoTitle])
	}

	supersedes, err := dc.overlapping(ctx, f, edits)
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("undo proposal %d: %w", number, err)
	}

	now := dc.d.Clock.Now().UTC()

	undo, err := dc.d.Proposals.UndoProposal(ctx, f, p.ID, current, domain.NewProposal{
		Title: title, Edits: edits, Supersedes: supersedes,
		Summary: "Reverses the change proposal " + strconv.FormatInt(p.Number, 10) + " published, against the current version.",
	}, now)
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("undo proposal %d: %w", number, err)
	}

	if err := dc.d.Audit.Append(ctx, domain.AuditEntry{
		At: now, Actor: by, Action: domain.AuditProposalUndo, TargetKind: domain.AuditTargetProposal, TargetID: string(undo.ID),
		FactoryID: f, Data: map[string]any{"number": undo.Number, "undoes": p.Number},
	}); err != nil {
		return domain.Proposal{}, fmt.Errorf("audit proposal %d: %w", undo.Number, err)
	}

	return undo, nil
}

// reversed are p's edits reversed, each checked to apply to its file now. A file p added, or
// whose edit changed nothing, is left out.
func (dc *Decider) reversed(ctx context.Context, f domain.FactoryID, p domain.Proposal) ([]domain.ProposalEdit, error) {
	var out []domain.ProposalEdit

	for _, e := range p.Edits {
		reps, err := dc.replacementsOf(ctx, f, p, e)
		if err != nil {
			return nil, err
		}

		if len(reps) == 0 {
			continue
		}

		undo := domain.ProposalEdit{Path: e.Path, Replacements: domain.ReverseReplacements(reps)}

		now, ok, err := dc.d.Authoring.CurrentFile(ctx, f, e.Path)
		if err != nil {
			return nil, fmt.Errorf("undo proposal %d: read %s: %w", p.Number, e.Path, err)
		}

		if _, err := undo.Apply(now, ok); err != nil {
			return nil, fmt.Errorf("undo proposal %d: %w: %w", p.Number, domain.ErrUndoConflict, err)
		}

		out = append(out, undo)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("undo proposal %d: no edit to reverse: %w", p.Number, domain.ErrUndoConflict)
	}

	return out, nil
}

// QueueReview queues a manual review of f on version, under policy (decision 2): it reads what
// finished since the last done review's window ended, or FirstWindow back (ADR 0039 decision 3). It refuses with
// domain.ErrLearningBudgetExhausted while Jonah's runs of the last BudgetWindow cost policy's
// share of the line's spend or more (ADR 0039 decision 16), and with domain.ErrReviewInFlight while one is pending or running.
func (dc *Decider) QueueReview(ctx context.Context, f domain.FactoryID, version domain.DefinitionVersionID, policy domain.JonahPolicy) (domain.JonahReview, error) {
	by, err := actor(ctx)
	if err != nil {
		return domain.JonahReview{}, err
	}

	now := dc.d.Clock.Now().UTC()

	spend, err := dc.d.Proposals.JonahSpendSince(ctx, f, now.Add(-BudgetWindow))
	if err != nil {
		return domain.JonahReview{}, fmt.Errorf("queue review: read spend: %w", err)
	}

	if b := policy.Budget(spend); b.Exhausted() {
		return domain.JonahReview{}, fmt.Errorf("queue review: $%.2f of $%.2f spent: %w", b.SpentUSD, b.BudgetUSD, domain.ErrLearningBudgetExhausted)
	}

	start, err := windowStart(ctx, dc.d.Proposals, f, now)
	if err != nil {
		return domain.JonahReview{}, fmt.Errorf("queue review: %w", err)
	}

	r, err := dc.d.Proposals.QueueJonahReview(ctx, domain.NewJonahReview{
		FactoryID: f, VersionID: version, Trigger: domain.JonahManual, WindowStart: start, WindowEnd: now, By: by, At: now,
	})
	if errors.Is(err, ports.ErrConflict) {
		return domain.JonahReview{}, fmt.Errorf("queue review: %w: %w", domain.ErrReviewInFlight, err)
	}

	if err != nil {
		return domain.JonahReview{}, fmt.Errorf("queue review: %w", err)
	}

	if err := dc.d.Audit.Append(ctx, domain.AuditEntry{
		At: now, Actor: by, Action: domain.AuditReviewQueued, TargetKind: domain.AuditTargetJonahReview, TargetID: string(r.ID),
		FactoryID: f, Data: map[string]any{"trigger": string(r.Trigger)},
	}); err != nil {
		return domain.JonahReview{}, fmt.Errorf("audit review %s: %w", r.ID, err)
	}

	return r, nil
}

// proposed is proposal number of f while it is proposed: ports.ErrProposalDecided after.
func (dc *Decider) proposed(ctx context.Context, f domain.FactoryID, number int64) (domain.Proposal, error) {
	p, err := dc.d.Proposals.Proposal(ctx, f, number)
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("read proposal %d: %w", number, err)
	}

	if p.State != domain.ProposalProposed {
		return domain.Proposal{}, fmt.Errorf("proposal %d is %s: %w", number, p.State, ports.ErrProposalDecided)
	}

	return p, nil
}

// decide records d, by the actor of ctx now, on proposal number of f.
func (dc *Decider) decide(ctx context.Context, f domain.FactoryID, number int64, d domain.ProposalDecision) (domain.Proposal, error) {
	by, err := actor(ctx)
	if err != nil {
		return domain.Proposal{}, err
	}

	d.By, d.At = by, dc.d.Clock.Now().UTC()

	p, err := dc.d.Proposals.DecideProposal(ctx, f, number, d)
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("record proposal %d %s: %w", number, d.State, err)
	}

	return p, nil
}

// audit appends action on p, by its decider, with data and its number.
func (dc *Decider) audit(ctx context.Context, p domain.Proposal, action string, data map[string]any) error {
	by, err := actor(ctx)
	if err != nil {
		return err
	}

	data["number"] = p.Number

	if err := dc.d.Audit.Append(ctx, domain.AuditEntry{
		At: p.DecidedAt, Actor: by, Action: action, TargetKind: domain.AuditTargetProposal, TargetID: string(p.ID),
		FactoryID: p.FactoryID, Data: data,
	}); err != nil {
		return fmt.Errorf("audit proposal %d: %w", p.Number, err)
	}

	return nil
}

// overlapping are f's open proposals whose edits overlap edits in the current version's files
// (domain.EditsOverlap): those a new proposal of edits supersedes.
func (dc *Decider) overlapping(ctx context.Context, f domain.FactoryID, edits []domain.ProposalEdit) ([]domain.ProposalID, error) {
	open, err := dc.d.Proposals.Proposals(ctx, ports.ProposalQuery{Factory: f, State: domain.ProposalProposed, Limit: reviewProposalLimit})
	if err != nil {
		return nil, fmt.Errorf("read open proposals: %w", err)
	}

	files := map[string][]byte{}

	for _, e := range edits {
		content, ok, err := dc.d.Authoring.CurrentFile(ctx, f, e.Path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Path, err)
		}

		if ok {
			files[e.Path] = content
		}
	}

	var out []domain.ProposalID

	for _, o := range open {
		if domain.EditsOverlap(o.Edits, edits, files) {
			out = append(out, o.ID)
		}
	}

	return out, nil
}

// replacementsOf are the replacements e of p made: its own, or for a whole file stored with the
// base it replaced, those that turn that base, in the version p was made against, into its
// content. A new file has none.
func (dc *Decider) replacementsOf(ctx context.Context, f domain.FactoryID, p domain.Proposal, e domain.ProposalEdit) ([]domain.Replacement, error) {
	switch {
	case !e.Whole():
		return e.Replacements, nil
	case e.BaseSHA256 == "":
		return nil, nil
	}

	base, ok, err := dc.d.Authoring.VersionFile(ctx, f, p.VersionID, e.Path)
	if err != nil {
		return nil, fmt.Errorf("undo proposal %d: read %s as it was: %w", p.Number, e.Path, err)
	}

	if !ok {
		return nil, fmt.Errorf("undo proposal %d: %s is not in the version it was made against: %w", p.Number, e.Path, domain.ErrUndoConflict)
	}

	return domain.ReplacementsOf(string(base), e.Content), nil
}

// editedFile is a file's content after a proposal's edit.
type editedFile struct {
	Path    string
	Content string
}

// fileReader reads path's content in one copy of f's definition; ok false when it has no such file.
type fileReader func(ctx context.Context, f domain.FactoryID, path string) (content []byte, ok bool, err error)

// applyEdits applies p's edits to the files read reads, in order: stale, naming the file, when
// one no longer applies.
func applyEdits(ctx context.Context, p domain.Proposal, read fileReader, f domain.FactoryID, stale error) ([]editedFile, error) {
	out := make([]editedFile, 0, len(p.Edits))

	for _, e := range p.Edits {
		content, ok, err := read(ctx, f, e.Path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Path, err)
		}

		applied, err := e.Apply(content, ok)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", stale, err)
		}

		out = append(out, editedFile{Path: e.Path, Content: applied})
	}

	return out, nil
}

// actor is the person of ctx a decision is made by.
func actor(ctx context.Context) (domain.Actor, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok || by.Kind == "" || by.ID == "" {
		return domain.Actor{}, errors.New("jonah decision: no actor in the context")
	}

	return by, nil
}

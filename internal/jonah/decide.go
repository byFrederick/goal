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
	// Head is the factory's current version (Current empty before the first publish) and whether
	// it has an open draft.
	Head(ctx context.Context, f domain.FactoryID) (Head, error)
	// CurrentFile is path's content in the current version; ok false when it has no such file.
	CurrentFile(ctx context.Context, f domain.FactoryID, path string) (content []byte, ok bool, err error)
	// DraftFile is path's content in the open draft; ok false when it has no such file.
	DraftFile(ctx context.Context, f domain.FactoryID, path string) (content []byte, ok bool, err error)
	// OpenDraft opens a draft from the current version, or returns the open one with created
	// false, with its revision.
	OpenDraft(ctx context.Context, f domain.FactoryID) (revision int64, created bool, err error)
	// WriteFile writes path into the open draft, whatever is there, and returns the draft's
	// revision after it.
	WriteFile(ctx context.Context, f domain.FactoryID, path string, content []byte) (revision int64, err error)
	// Publish publishes the draft at revision while expected is the current version. After a
	// refusal, kept reports whether the draft stays open by design (the eval gate's), so it must
	// not be discarded.
	Publish(ctx context.Context, f domain.FactoryID, revision int64, expected domain.DefinitionVersionID, message string) (v domain.DefinitionVersion, kept bool, err error)
	// DiscardDraft discards the draft at revision.
	DiscardDraft(ctx context.Context, f domain.FactoryID, revision int64) error
}

// Head is a factory's current version and whether a draft is open.
type Head struct {
	Current   domain.DefinitionVersionID
	DraftOpen bool
}

// Proposals are the reads and writes of ports.JonahStore a decision makes.
type Proposals interface {
	Proposal(ctx context.Context, factory domain.FactoryID, number int64) (domain.Proposal, error)
	DecideProposal(ctx context.Context, factory domain.FactoryID, number int64, d domain.ProposalDecision) (domain.Proposal, error)
	QueueJonahReview(ctx context.Context, n domain.NewJonahReview) (domain.JonahReview, error)
	JonahReviews(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.JonahReview, error)
	JonahSpendSince(ctx context.Context, factory domain.FactoryID, since time.Time) (float64, error)
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
	// BudgetWindow is how far back the weekly budget counts review runs (decision 2).
	BudgetWindow = 7 * 24 * time.Hour
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

// Approve publishes proposal number of f as a new version (decision 7): it opens a draft from the
// current version, writes the proposal's files and publishes it. It refuses with
// domain.ErrProposalDraftOpen while a draft is open, domain.ErrProposalStale when a file's
// current SHA-256 is not its base, and ports.ErrProposalDecided once the proposal is decided; a
// publish refusal is returned as authoring gave it, with the draft discarded unless the publish
// keeps it.
func (dc *Decider) Approve(ctx context.Context, f domain.FactoryID, number int64) (domain.Proposal, domain.DefinitionVersion, error) {
	p, err := dc.proposed(ctx, f, number)
	if err != nil {
		return domain.Proposal{}, domain.DefinitionVersion{}, err
	}

	head, err := dc.d.Authoring.Head(ctx, f)
	if err != nil {
		return domain.Proposal{}, domain.DefinitionVersion{}, fmt.Errorf("approve proposal %d: %w", number, err)
	}

	if head.DraftOpen {
		return domain.Proposal{}, domain.DefinitionVersion{}, fmt.Errorf("approve proposal %d: %w", number, domain.ErrProposalDraftOpen)
	}

	if head.Current == "" {
		return domain.Proposal{}, domain.DefinitionVersion{}, fmt.Errorf("approve proposal %d: no current version: %w", number, domain.ErrProposalStale)
	}

	for _, file := range p.Files {
		content, ok, err := dc.d.Authoring.CurrentFile(ctx, f, file.Path)
		if err != nil {
			return domain.Proposal{}, domain.DefinitionVersion{}, fmt.Errorf("approve proposal %d: read %s: %w", number, file.Path, err)
		}

		if !matchesBase(file, content, ok) {
			return domain.Proposal{}, domain.DefinitionVersion{}, fmt.Errorf("approve proposal %d: %s: %w", number, file.Path, domain.ErrProposalStale)
		}
	}

	revision, created, err := dc.d.Authoring.OpenDraft(ctx, f)
	if err != nil {
		return domain.Proposal{}, domain.DefinitionVersion{}, fmt.Errorf("approve proposal %d: open draft: %w", number, err)
	}

	if !created { // another writer opened one since Head
		return domain.Proposal{}, domain.DefinitionVersion{}, fmt.Errorf("approve proposal %d: %w", number, domain.ErrProposalDraftOpen)
	}

	v, err := dc.publish(ctx, f, p, revision, head.Current)
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

// publish writes p's files into the draft at revision and publishes it, discarding the draft
// after a refusal that does not keep it.
func (dc *Decider) publish(ctx context.Context, f domain.FactoryID, p domain.Proposal, revision int64, expected domain.DefinitionVersionID) (domain.DefinitionVersion, error) {
	v, kept, err := func() (domain.DefinitionVersion, bool, error) {
		for _, file := range p.Files {
			r, err := dc.d.Authoring.WriteFile(ctx, f, file.Path, []byte(file.Content))
			if err != nil {
				return domain.DefinitionVersion{}, false, fmt.Errorf("write %s: %w", file.Path, err)
			}

			revision = r
		}

		return dc.d.Authoring.Publish(ctx, f, revision, expected, "Proposal "+strconv.FormatInt(p.Number, 10)+": "+p.Title)
	}()
	if err == nil {
		return v, nil
	}

	if !kept {
		if derr := dc.d.Authoring.DiscardDraft(ctx, f, revision); derr != nil {
			dc.log.WarnContext(ctx, "jonah proposal draft not discarded", slog.String("factory", string(f)),
				slog.Int64("proposal", p.Number), slog.Any("error", derr))
		}
	}

	return domain.DefinitionVersion{}, fmt.Errorf("approve proposal %d: %w", p.Number, err)
}

// Edit writes proposal number's files into f's draft, opening one from the current version when
// none is open, and sets it applied (decision 8). It refuses with domain.ErrProposalDraftConflict,
// writing nothing, when the draft's copy of a file is not its base, and with
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

	for _, file := range p.Files {
		content, ok, err := dc.d.Authoring.DraftFile(ctx, f, file.Path)
		if err != nil {
			return domain.Proposal{}, 0, fmt.Errorf("edit proposal %d: read %s: %w", number, file.Path, err)
		}

		if !matchesBase(file, content, ok) {
			dc.discardOpened(ctx, f, number, revision, created)

			return domain.Proposal{}, 0, fmt.Errorf("edit proposal %d: %s: %w", number, file.Path, domain.ErrProposalDraftConflict)
		}
	}

	for _, file := range p.Files {
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

// QueueReview queues a manual review of f on version, under policy (decision 2): it reads the
// sent-back touches since the last review's window ended, or FirstWindow back. It refuses with
// domain.ErrLearningBudgetExhausted while the review runs of the last BudgetWindow cost
// policy.WeeklyUSD or more, and with domain.ErrReviewInFlight while one is pending or running.
func (dc *Decider) QueueReview(ctx context.Context, f domain.FactoryID, version domain.DefinitionVersionID, policy domain.JonahPolicy) (domain.JonahReview, error) {
	by, err := actor(ctx)
	if err != nil {
		return domain.JonahReview{}, err
	}

	now := dc.d.Clock.Now().UTC()

	spent, err := dc.d.Proposals.JonahSpendSince(ctx, f, now.Add(-BudgetWindow))
	if err != nil {
		return domain.JonahReview{}, fmt.Errorf("queue review: read spend: %w", err)
	}

	if spent >= policy.WeeklyUSD {
		return domain.JonahReview{}, fmt.Errorf("queue review: $%.2f of $%.2f spent: %w", spent, policy.WeeklyUSD, domain.ErrLearningBudgetExhausted)
	}

	start := now.Add(-FirstWindow)

	last, err := dc.d.Proposals.JonahReviews(ctx, f, 1)
	if err != nil {
		return domain.JonahReview{}, fmt.Errorf("queue review: read the last review: %w", err)
	}

	if len(last) > 0 && !last[0].WindowEnd.IsZero() && last[0].WindowEnd.Before(now) {
		start = last[0].WindowEnd
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

// matchesBase reports whether a file's content (present says whether it exists) is the one
// file was made against: absent for a new file, else of SHA-256 base_sha256.
func matchesBase(file domain.ProposalFile, content []byte, present bool) bool {
	if file.BaseSHA256 == "" {
		return !present
	}

	return present && domain.HashBlob(content) == domain.BlobHash(file.BaseSHA256)
}

// actor is the person of ctx a decision is made by.
func actor(ctx context.Context) (domain.Actor, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok || by.Kind == "" || by.ID == "" {
		return domain.Actor{}, errors.New("jonah decision: no actor in the context")
	}

	return by, nil
}

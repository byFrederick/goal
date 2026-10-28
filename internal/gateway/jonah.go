package gateway

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Jonah reads what a Jonah review's bundle holds besides its definition files (ADR 0034
// decision 1): the factory's sent-back events in the review's window and its rejected proposals.
// ports.JonahStore satisfies it.
type Jonah interface {
	SentBackBetween(ctx context.Context, factory domain.FactoryID, since, until time.Time, limit int) ([]domain.SentBack, error)
	Proposals(ctx context.Context, q ports.ProposalQuery) ([]domain.Proposal, error)
}

// errNoJonah answers a jonah token's bundle when the gateway has no Deps.Jonah: 503.
var errNoJonah = errors.New("the gateway has no jonah store")

const (
	// jonahEventLimit caps the sent-back events a review reads.
	jonahEventLimit = 200
	// rejectedWithin is how far back a review reads the factory's rejected proposals.
	rejectedWithin = 90 * 24 * time.Hour
	// rejectedPage is how many rejected proposals one Proposals call reads; rejectedScan caps
	// how many are read in all and rejectedLimit how many the bundle holds.
	rejectedPage  = 50
	rejectedScan  = 500
	rejectedLimit = 50
)

// bundleJonah is spec.jonah, what goal-agent's jonah mode reads (apps/worker/src/spec.ts Jonah).
type bundleJonah struct {
	Files    []jonahFile     `json:"files"`
	Events   []jonahEvent    `json:"events"`
	Rejected []jonahRejected `json:"rejected"`
	// ProposeAfter is the policy's proposeAfter; Manual whether a person asked for the review.
	ProposeAfter int  `json:"propose_after"`
	Manual       bool `json:"manual"`
}

// jonahFile is one definition file a review reads: its path, sha256 and text.
type jonahFile struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Content string `json:"content"`
}

// jonahEvent is one sent-back touch in the review's window; a proposal's evidence names its id.
type jonahEvent struct {
	ID      string `json:"id"`
	ItemRef string `json:"item_ref"`
	Title   string `json:"title"`
	Station string `json:"station"`
	Agent   string `json:"agent"`
	Kind    string `json:"kind"`
	Reason  string `json:"reason"`
}

// jonahRejected is a proposal the factory rejected recently, with the reason a person gave.
type jonahRejected struct {
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Paths   []string `json:"paths"`
	Reason  string   `json:"reason"`
}

// jonahBundle is the bundle of a Jonah review's run: the files of c's version that a proposal may
// edit, the factory's sent-back events in c's window and its proposals rejected within
// rejectedWithin of now. It names no item, repository, connection, knowledge source or rule, and
// records nothing. The version must be c's factory's in c's organization (ctx).
func (g *Gateway) jonahBundle(ctx context.Context, c domain.TaskClaims) (runBundle, error) {
	if g.jonah == nil {
		return runBundle{}, errNoJonah
	}

	f, err := g.defs.Version(ctx, c.VersionID)
	if err != nil {
		return runBundle{}, fmt.Errorf("load factory %s version %s: %w", c.FactoryID, c.VersionID, err)
	}

	if f.Metadata.ID != string(c.FactoryID) {
		return runBundle{}, fmt.Errorf("%w: version %s is not a version of factory %s", errForbidden, c.VersionID, c.FactoryID)
	}

	line := f.Line()
	j := &bundleJonah{ProposeAfter: line.Jonah.ProposeAfter, Manual: c.Manual}

	if j.Files, err = g.jonahFiles(ctx, c.VersionID); err != nil {
		return runBundle{}, err
	}

	if j.Events, err = g.jonahEvents(ctx, c); err != nil {
		return runBundle{}, err
	}

	if j.Rejected, err = g.jonahRejected(ctx, c.FactoryID); err != nil {
		return runBundle{}, err
	}

	return runBundle{
		Spec: bundleSpec{
			FactoryID: c.FactoryID, RunID: c.RunID, Agent: c.Agent, Model: cmp.Or(c.Model, line.Jonah.Model), Purpose: line.Purpose,
			Skills: []string{}, ToolsAllow: []string{}, ToolsDeny: []string{}, Connections: []bundleConnection{}, Rules: []bundleRule{},
			GrantedCalls: []grantedCall{}, Jonah: j,
		},
		Files: map[string][]byte{},
		Git:   []gitCredential{},
		Tools: []string{},
	}, nil
}

// jonahFiles are version id's files at a path a proposal may edit (domain.ProposalPathAllowed),
// sorted by path. A file that is not UTF-8 text is left out: the review reads text only.
func (g *Gateway) jonahFiles(ctx context.Context, id domain.DefinitionVersionID) ([]jonahFile, error) {
	files, err := g.defs.VersionFiles(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("files of version %s: %w", id, err)
	}

	out := []jonahFile{}

	for _, p := range slices.Sorted(maps.Keys(files)) {
		if !domain.ProposalPathAllowed(p) || !utf8.Valid(files[p]) {
			continue
		}

		out = append(out, jonahFile{Path: p, SHA256: string(domain.HashBlob(files[p])), Content: string(files[p])})
	}

	return out, nil
}

// jonahEvents are the factory's sent-back touches in c's window, oldest first.
func (g *Gateway) jonahEvents(ctx context.Context, c domain.TaskClaims) ([]jonahEvent, error) {
	sent, err := g.jonah.SentBackBetween(ctx, c.FactoryID, time.Unix(c.WindowStart, 0), time.Unix(c.WindowEnd, 0), jonahEventLimit)
	if err != nil {
		return nil, fmt.Errorf("sent-back events of factory %s: %w", c.FactoryID, err)
	}

	out := make([]jonahEvent, 0, len(sent))
	for _, s := range sent {
		out = append(out, jonahEvent{
			ID: strconv.FormatInt(s.EventID, 10), ItemRef: "#" + strconv.FormatInt(s.ItemNumber, 10), Title: s.ItemTitle,
			Station: s.Station, Agent: s.Agent, Kind: string(s.Kind), Reason: s.Reason,
		})
	}

	return out, nil
}

// jonahRejected are the factory's proposals rejected within rejectedWithin of now, newest
// numbered first, at most rejectedLimit of the latest rejectedScan rejected proposals. A
// proposal's number orders its creation, not its decision, so every page up to the scan cap is
// read rather than stopping at the first old decision.
func (g *Gateway) jonahRejected(ctx context.Context, factory domain.FactoryID) ([]jonahRejected, error) {
	since := g.clock.Now().Add(-rejectedWithin)
	out := []jonahRejected{}

	for before, scanned := int64(0), 0; scanned < rejectedScan && len(out) < rejectedLimit; {
		page, err := g.jonah.Proposals(ctx, ports.ProposalQuery{Factory: factory, State: domain.ProposalRejected, Before: before, Limit: rejectedPage})
		if err != nil {
			return nil, fmt.Errorf("rejected proposals of factory %s: %w", factory, err)
		}

		for _, p := range page {
			if !p.DecidedAt.Before(since) && len(out) < rejectedLimit {
				out = append(out, jonahRejected{Title: p.Title, Summary: p.Summary, Paths: nonNil(p.Paths()), Reason: p.RejectReason})
			}
		}

		if len(page) < rejectedPage {
			break
		}

		scanned += len(page)
		before = page[len(page)-1].Number
	}

	return out, nil
}

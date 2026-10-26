// Package authoring is goald's write side of factory definitions: creating a factory (blank,
// from a template, by cloning one or by importing files), its one shared draft saved with
// optimistic concurrency (Visual sections, single files or the whole tree), validation that
// runs the config checks and those of later phases, publishing with station moves, restore,
// discard, pause, compare and the template catalog.
//
// Every method acts in the organization of ctx (domain.WithOrg) as the actor of ctx
// (domain.WithActor); the API sets both. Methods take a factory's id: the API resolves a
// {factory} reference with Factory first. Refusals are the errors below; ProblemCode names the
// problem detail code the API answers with.
package authoring

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/factorymodel"
	"github.com/byFrederick/goal/internal/ports"
)

// The factory model's types, so the API reaches them through this package (it may not import
// factorymodel).
type (
	// Section is one tab of the Visual editor.
	Section = factorymodel.Section
	// Model is the factory model the Visual editor edits.
	Model = factorymodel.Model
	// Ranges maps model pointers to factory.yaml lines.
	Ranges = factorymodel.Ranges
	// Comparison is the semantic difference between two versions.
	Comparison = factorymodel.Comparison
	// ParseError is a factory.yaml that does not parse: problem yaml-invalid.
	ParseError = factorymodel.ParseError
)

// The sections, in the editor's order.
const (
	Basics      = factorymodel.Basics
	Line        = factorymodel.Line
	Agents      = factorymodel.Agents
	Connections = factorymodel.Connections
	Review      = factorymodel.Review
)

// Refusals. ProblemCode maps each, and the errors they are wrapped in, to its problem code; the
// status the API answers with is in brackets.
var (
	// ErrFactoryNotFound: no factory of the organization has the id or name [404].
	ErrFactoryNotFound = errors.New("factory not found")
	// ErrNoDraft: the factory has no open draft [404].
	ErrNoDraft = errors.New("no open draft")
	// ErrVersionNotFound: no such version, or a selector that names none (base of a first
	// draft) [404].
	ErrVersionNotFound = errors.New("version not found")
	// ErrFileNotFound: the version has no file at the path [404].
	ErrFileNotFound = errors.New("file not found")
	// ErrTemplateNotFound: the catalog has no such template or version [404].
	ErrTemplateNotFound = errors.New("template not found")
	// ErrInvalidRequest: a malformed request: an unknown section or selector, a missing name, a
	// station move to a station the new version lacks, a message over 200 characters [400].
	ErrInvalidRequest = errors.New("invalid request")
	// ErrDraftExists: a draft on another base is open and the request did not replace it [409].
	ErrDraftExists = errors.New("another draft is open")
	// ErrDraftConflict: the draft, section or file changed since the caller read it; the error
	// is a *SectionConflictError or a *FileConflictError when the current value is known [412].
	ErrDraftConflict = errors.New("draft changed")
	// ErrBaseMoved: the current version is not the one the caller expected [409].
	ErrBaseMoved = ports.ErrBaseMoved
	// ErrNameTaken: another factory of the organization has the name [409].
	ErrNameTaken = ports.ErrNameTaken
	// ErrFactoryExists: an import keeping its id names a factory that exists [409].
	ErrFactoryExists = errors.New("factory exists")
	// ErrFactoryPublished: only a factory that was never published can be deleted [409].
	ErrFactoryPublished = errors.New("factory published")
	// ErrDefinitionInvalid: publish refused because of error problems; the error is an
	// *InvalidError [409].
	ErrDefinitionInvalid = errors.New("definition invalid")
	// ErrItemsAtRemovedStation: publish or restore would drop stations open items sit at; the
	// error is a *ports.ItemsAtRemovedStationError [409].
	ErrItemsAtRemovedStation = ports.ErrItemsAtRemovedStation
	// ErrFactoryRunning: archiving a factory that is not paused [409].
	ErrFactoryRunning = ports.ErrFactoryRunning
	// ErrFactoryBusy: archiving or purging a factory whose items hold leases; the error is a
	// *ports.FactoryBusyError [409].
	ErrFactoryBusy = ports.ErrFactoryBusy
	// ErrFactoryNotArchived: purging a factory that is not archived [409].
	ErrFactoryNotArchived = ports.ErrFactoryNotArchived
	// ErrModelInvalid: a section value of the wrong shape [422].
	ErrModelInvalid = factorymodel.ErrModelInvalid
	// ErrImportInvalid: imported files without a factory.yaml that parses [422].
	ErrImportInvalid = errors.New("import invalid")
	// ErrBadPath: a file path outside the rules (Paths) [422].
	ErrBadPath = errors.New("bad path")
	// ErrTooLarge: a file over 1 MiB, more than 1,000 files or 16 MiB in a tree [413].
	ErrTooLarge = errors.New("too large")
)

// Problem codes of the refusals.
const (
	CodeFactoryNotFound    = "factory-not-found"
	CodeNoDraft            = "no-draft"
	CodeVersionNotFound    = "version-not-found"
	CodeFileNotFound       = "file-not-found"
	CodeTemplateNotFound   = "template-not-found"
	CodeInvalidRequest     = "invalid-request"
	CodeDraftExists        = "draft-exists"
	CodeDraftConflict      = "draft-conflict"
	CodeBaseMoved          = "draft-base-moved"
	CodeNameTaken          = "name-taken"
	CodeFactoryExists      = "factory-exists"
	CodeFactoryPublished   = "factory-published"
	CodeDefinitionInvalid  = "definition-invalid"
	CodeItemsAtRemoved     = "items-at-removed-station"
	CodeFactoryRunning     = "factory-running"
	CodeFactoryBusy        = "factory-busy"
	CodeFactoryNotArchived = "factory-not-archived"
	CodeYAMLInvalid        = "yaml-invalid"
	CodeModelInvalid       = "model-invalid"
	CodeImportInvalid      = "import-invalid"
	CodeBadPath            = "bad-path"
	CodeTooLarge           = "too-large"
)

var problemCodes = []struct {
	err  error
	code string
}{
	{ErrFactoryNotFound, CodeFactoryNotFound},
	{ErrNoDraft, CodeNoDraft},
	{ErrVersionNotFound, CodeVersionNotFound},
	{ErrFileNotFound, CodeFileNotFound},
	{ErrTemplateNotFound, CodeTemplateNotFound},
	{ErrInvalidRequest, CodeInvalidRequest},
	{ErrDraftExists, CodeDraftExists},
	{ErrDraftConflict, CodeDraftConflict},
	{ErrBaseMoved, CodeBaseMoved},
	{ErrNameTaken, CodeNameTaken},
	{ErrFactoryExists, CodeFactoryExists},
	{ErrFactoryPublished, CodeFactoryPublished},
	{ErrDefinitionInvalid, CodeDefinitionInvalid},
	{ErrItemsAtRemovedStation, CodeItemsAtRemoved},
	{ErrFactoryRunning, CodeFactoryRunning},
	{ErrFactoryBusy, CodeFactoryBusy},
	{ErrFactoryNotArchived, CodeFactoryNotArchived},
	{ErrModelInvalid, CodeModelInvalid},
	{ErrImportInvalid, CodeImportInvalid},
	{ErrBadPath, CodeBadPath},
	{ErrTooLarge, CodeTooLarge},
}

// ProblemCode is the problem detail code of a refusal (a *ParseError is yaml-invalid, unless
// it is the reason of another refusal such as import-invalid), or "" for any other error,
// which is the server's fault.
func ProblemCode(err error) string {
	for _, c := range problemCodes {
		if errors.Is(err, c.err) {
			return c.code
		}
	}

	if _, ok := errors.AsType[*ParseError](err); ok {
		return CodeYAMLInvalid
	}

	return ""
}

// SectionConflictError is a stale section save: Value and ETag are the section as it is now,
// for the editor's "Keep theirs / Overwrite".
type SectionConflictError struct {
	Section Section
	Value   any
	ETag    string
}

func (e *SectionConflictError) Error() string {
	return fmt.Sprintf("section %s changed (now %s)", e.Section, e.ETag)
}

// Unwrap makes the error match ErrDraftConflict.
func (e *SectionConflictError) Unwrap() error { return ErrDraftConflict }

// FileConflictError is a stale file save: SHA256 is the file's hash now, "" when it is absent.
type FileConflictError struct {
	Path   string
	SHA256 domain.BlobHash
}

func (e *FileConflictError) Error() string {
	return fmt.Sprintf("file %s changed", e.Path)
}

// Unwrap makes the error match ErrDraftConflict.
func (e *FileConflictError) Unwrap() error { return ErrDraftConflict }

// InvalidError is a publish refused for its error problems; Problems lists every problem.
type InvalidError struct {
	Problems []domain.DefinitionProblem
}

func (e *InvalidError) Error() string {
	n := 0

	for _, p := range e.Problems {
		if p.Blocking() {
			n++
		}
	}

	return fmt.Sprintf("definition has %d error problems", n)
}

// Unwrap makes the error match ErrDefinitionInvalid.
func (e *InvalidError) Unwrap() error { return ErrDefinitionInvalid }

// Loads reads the open items per station (ports.Store has it).
type Loads interface {
	StationLoads(ctx context.Context, factory domain.FactoryID) ([]domain.StationLoad, error)
}

// Deps is what the service works with. Store, Templates and Clock are required.
type Deps struct {
	Store ports.DefinitionStore
	// Loads, when set, lets validation report open items at stations a draft removes.
	Loads Loads
	// Templates is the bucket the template catalog (catalog/templates/) is read from.
	Templates ports.ObjectStore
	// Credentials is the connections check: ConnectionsCheck; nil skips it.
	// Nil leaves the credentials check out.
	Credentials Credentials
	Clock       ports.Clock
	// NewID returns a new factory id (a UUIDv7); uuid.NewV7 when nil.
	NewID func() string
}

// Service implements definition authoring over a DefinitionStore. It is safe for concurrent
// use; it keeps no state but the template index, a cache of the immutable catalog.
type Service struct {
	store       ports.DefinitionStore
	loads       Loads
	credentials Credentials
	clock       ports.Clock
	newID       func() string
	templates   *TemplateCatalog
}

// New returns the service.
func New(d Deps) *Service {
	newID := d.NewID
	if newID == nil {
		newID = func() string { return uuid.Must(uuid.NewV7()).String() }
	}

	return &Service{
		store: d.Store, loads: d.Loads, credentials: d.Credentials,
		clock: d.Clock, newID: newID,
		templates: NewTemplateCatalog(d.Templates, d.Clock),
	}
}

// Factories lists the organization's factories by name.
func (s *Service) Factories(ctx context.Context) ([]ports.FactoryRecord, error) {
	recs, err := s.store.FactoryRecords(ctx)
	if err != nil {
		return nil, fmt.Errorf("list factories: %w", err)
	}

	return recs, nil
}

// Factory finds a factory of the organization by id or by name (without case):
// ErrFactoryNotFound otherwise.
func (s *Service) Factory(ctx context.Context, ref string) (ports.FactoryRecord, error) {
	if _, err := uuid.Parse(ref); err == nil {
		rec, err := s.record(ctx, domain.FactoryID(strings.ToLower(ref)))
		if err == nil || !errors.Is(err, ErrFactoryNotFound) {
			return rec, err
		}
	}

	recs, err := s.Factories(ctx)
	if err != nil {
		return ports.FactoryRecord{}, err
	}

	for _, r := range recs {
		if strings.EqualFold(r.Name, ref) {
			return r, nil
		}
	}

	return ports.FactoryRecord{}, fmt.Errorf("factory %q: %w", ref, ErrFactoryNotFound)
}

// record reads a factory by id, ErrFactoryNotFound when the organization has none.
func (s *Service) record(ctx context.Context, f domain.FactoryID) (ports.FactoryRecord, error) {
	rec, err := s.store.FactoryRecord(ctx, f)
	if errors.Is(err, ports.ErrNotFound) {
		return ports.FactoryRecord{}, fmt.Errorf("factory %s: %w", f, ErrFactoryNotFound)
	}

	if err != nil {
		return ports.FactoryRecord{}, fmt.Errorf("read factory %s: %w", f, err)
	}

	return rec, nil
}

// draft reads a factory's open draft, ErrNoDraft (or ErrFactoryNotFound) when there is none.
func (s *Service) draft(ctx context.Context, f domain.FactoryID) (domain.DefinitionVersion, error) {
	d, err := s.store.Draft(ctx, f)
	if errors.Is(err, ports.ErrNotFound) {
		if _, rerr := s.record(ctx, f); rerr != nil {
			return domain.DefinitionVersion{}, rerr
		}

		return domain.DefinitionVersion{}, fmt.Errorf("factory %s: %w", f, ErrNoDraft)
	}

	if err != nil {
		return domain.DefinitionVersion{}, fmt.Errorf("read draft of %s: %w", f, err)
	}

	return d, nil
}

// errNoActor is a context without an actor: a wiring bug, since the API sets one on every
// request.
var errNoActor = errors.New("no actor in context")

func actor(ctx context.Context) (domain.Actor, error) {
	a, ok := domain.ActorFrom(ctx)
	if !ok {
		return domain.Actor{}, errNoActor
	}

	return a, nil
}

// maxSaveAttempts bounds the read, check, apply and compare-and-set cycle of a save when other
// writers keep moving the draft's revision.
const maxSaveAttempts = 3

// templateTTL is how long the template index is used before the catalog is listed again.
const templateTTL = 5 * time.Minute

package moveover

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Actor is who the conversion acts as: Goal publishes the converted versions.
var Actor = domain.Actor{Kind: domain.ActorSystem, ID: "goal", Name: "Goal"}

// Publisher publishes a draft with the publish checks: authoring.Service.
type Publisher interface {
	Publish(ctx context.Context, f domain.FactoryID, r authoring.PublishRequest) (domain.DefinitionVersion, error)
}

// Orgs lists the install's organizations: ports.Tenancy.
type Orgs interface {
	ActiveOrgs(ctx context.Context) ([]domain.OrgID, error)
}

// Deps is what Run works with; every one is required but CheckTimeout.
type Deps struct {
	Orgs        Orgs
	Connections ports.Connections
	Secrets     ports.OrgSecrets
	Knowledge   ports.Knowledge
	Definitions ports.DefinitionStore
	Publisher   Publisher
	Conversions ports.FactoryConversions
	// CodeHosts are the code host adapters by forge (github, gitlab): a converted version's
	// repository token is tested on its repository's.
	CodeHosts map[string]ports.CodeHost
	// Tools lists a converted environment file's remote MCP servers' tools.
	Tools ports.ToolLister
	// CheckTimeout bounds each check's call to a host; CheckTimeout when zero.
	CheckTimeout time.Duration
	Clock        ports.Clock
	Logger       *slog.Logger
}

// Mover converts every organization.
type Mover struct {
	d Deps
}

// New returns the mover.
func New(d Deps) *Mover { return &Mover{d: d} }

// Tally is what the conversion did in one organization.
type Tally struct {
	Converted, Refused, Secrets int
	// Sources are the knowledge sources pointed at a secret.
	Sources int
	// Kinds are the synced sources converted into repository or Confluence sources, Live the live
	// sources deleted and Due the sources without a snapshot made due (ADR 0038 decision 16).
	Kinds, Live, Due int
}

// Run converts every organization once, under the install's conversion lock, and logs one line
// per organization (moved over). Another goald holding the lock runs it instead; a converted
// factory is never converted again, so a second Run changes nothing, and a refused one is tried
// again. An organization that fails is logged and the others go on.
func (m *Mover) Run(ctx context.Context) error {
	ran, err := m.d.Conversions.WithLock(ctx, func(ctx context.Context) error {
		orgs, err := m.d.Orgs.ActiveOrgs(ctx)
		if err != nil {
			return fmt.Errorf("move over: list organizations: %w", err)
		}

		for _, org := range orgs {
			t, err := m.Org(domain.WithActor(domain.WithOrg(ctx, org), Actor))
			if err != nil {
				m.d.Logger.ErrorContext(ctx, "move over failed", slog.String("org", string(org)), slog.Any("error", err))

				continue
			}

			m.d.Logger.InfoContext(ctx, "moved over", slog.String("org", string(org)),
				slog.Int("converted", t.Converted), slog.Int("refused", t.Refused), slog.Int("secrets", t.Secrets), slog.Int("sources", t.Sources),
				slog.Int("kinds", t.Kinds), slog.Int("live", t.Live), slog.Int("due", t.Due))
		}

		return nil
	})
	if err != nil {
		return err
	}

	if !ran {
		m.d.Logger.InfoContext(ctx, "move over skipped", slog.String("reason", "another goald holds the conversion lock"))
	}

	return nil
}

// Org converts the organization in ctx: its connections' secrets first, then its synced
// knowledge sources still on a connection, then each factory with no conversion recorded whose
// current version is on connections, and last its knowledge sources into ADR 0038's kinds, after
// the factories so a factory listing a live source still publishes its conversion.
func (m *Mover) Org(ctx context.Context) (Tally, error) {
	var t Tally

	conns, err := m.opened(ctx)
	if err != nil {
		return t, err
	}

	if conns, t.Secrets, err = m.secrets(ctx, conns); err != nil {
		return t, err
	}

	if t.Sources, err = m.sources(ctx, conns); err != nil {
		return t, err
	}

	if err := m.factories(ctx, conns, &t); err != nil {
		return t, err
	}

	return t, m.knowledge(ctx, &t)
}

// knowledge moves the organization's knowledge sources onto files (ADR 0038 decision 16): each
// synced source on a secret becomes the kind its reader reads (ConvertSource, which makes it due),
// each live source is deleted, and each other source without a snapshot that is not due nor
// syncing is made due, so every source syncs once. A synced source it cannot convert is logged
// (knowledge source not converted) with the reason, never a value, and left as it is. It never
// edits a definition: a factory listing a converted source lists it by the same name. A second
// run finds nothing to do.
func (m *Mover) knowledge(ctx context.Context, t *Tally) error {
	list, err := m.d.Knowledge.Sources(ctx)
	if err != nil {
		return fmt.Errorf("list knowledge sources: %w", err)
	}

	now := m.d.Clock.Now()

	for _, src := range list {
		if err := m.moveSource(ctx, src, now, t); err != nil {
			return err
		}
	}

	return nil
}

// moveSource converts, deletes or makes due one source, counting it in t.
func (m *Mover) moveSource(ctx context.Context, src domain.KnowledgeSource, now time.Time, t *Tally) error {
	switch src.Kind {
	case domain.SourceLive:
		if err := m.d.Knowledge.DeleteSource(ctx, src.Name, Actor, nil); err != nil {
			return fmt.Errorf("delete live knowledge source %s: %w", src.Name, err)
		}

		t.Live++

		return nil
	case domain.SourceSynced:
		if kind, err := src.ConvertsTo(); err == nil {
			if _, err := m.d.Knowledge.ConvertSource(ctx, src.Name, kind, Actor); err != nil {
				return fmt.Errorf("convert knowledge source %s: %w", src.Name, err)
			}

			t.Kinds++

			return nil
		}

		m.d.Logger.WarnContext(ctx, "knowledge source not converted", slog.String("source", src.Name), slog.String("reason", notConverted(src)))
	case domain.SourceUpload, domain.SourceRepository, domain.SourceConfluence:
		// Already of ADR 0038's kinds: due once below when it has no snapshot.
	}

	due := src.NextSyncAt != nil && !src.NextSyncAt.After(now)
	if src.CurrentSnapshot != "" || due || src.Status == domain.SyncSyncing {
		return nil
	}

	if err := m.d.Knowledge.SyncNow(ctx, src.Name); err != nil {
		return fmt.Errorf("make knowledge source %s due: %w", src.Name, err)
	}

	t.Due++

	return nil
}

// notConverted is why the synced source src does not convert.
func notConverted(src domain.KnowledgeSource) string {
	if src.Secret == "" {
		return "it reads its connection and no secret; give it a secret, or recreate it as a repository or Confluence source"
	}

	return "its reader " + src.Config[domain.SourceReaderKey] + " is neither gitdocs nor confluence"
}

// factories converts each factory not converted yet whose current version is on connections,
// counting them in t. A refusal replaces the one recorded before, unless it is the same: then
// nothing is recorded or counted, so Needs you keeps the entry as it was.
func (m *Mover) factories(ctx context.Context, conns []domain.ResolvedConnection, t *Tally) error {
	done, err := m.d.Conversions.List(ctx)
	if err != nil {
		return fmt.Errorf("list conversions: %w", err)
	}

	recs, err := m.d.Definitions.FactoryRecords(ctx)
	if err != nil {
		return fmt.Errorf("list factories: %w", err)
	}

	for _, rec := range recs {
		prev, due := pending(done, rec)
		if !due {
			continue
		}

		c, err := m.factory(ctx, rec, conns)
		if err != nil {
			return fmt.Errorf("factory %s: %w", rec.ID, err)
		}

		if c == nil || prev != nil && c.Refused() && c.From == prev.From && slices.Equal(c.Problems, prev.Problems) {
			continue
		}

		if _, err := m.d.Conversions.Record(ctx, *c); err != nil {
			return err
		}

		if c.Refused() {
			t.Refused++
		} else {
			t.Converted++
		}
	}

	return nil
}

// pending reports whether rec is due a conversion: it has a current version, is not archived and
// is not converted yet. prev is its refusal recorded before, if any.
func pending(done []ports.FactoryConversion, rec ports.FactoryRecord) (prev *ports.FactoryConversion, due bool) {
	if rec.Current == nil || rec.Archived() {
		return nil, false
	}

	i := slices.IndexFunc(done, func(c ports.FactoryConversion) bool { return c.Factory == rec.ID })
	if i < 0 {
		return nil, true
	}

	return &done[i], done[i].Refused()
}

// opened are the organization's connections with their values. A failure names the connection,
// never a value.
func (m *Mover) opened(ctx context.Context) ([]domain.ResolvedConnection, error) {
	list, err := m.d.Connections.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list connections: %w", err)
	}

	out := make([]domain.ResolvedConnection, 0, len(list))

	for _, c := range list {
		rc, err := m.d.Connections.Open(ctx, c.Name)
		if err != nil {
			return nil, fmt.Errorf("open connection %s: %w", c.Name, err)
		}

		out = append(out, rc)
	}

	return out, nil
}

// secrets creates the secrets conns become and counts the ones it created. A secret that has the
// name already, is open to every factory and holds the connection's value is the connection's
// secret: it is reused. Any other keeps its name, and the connection's secret takes the first
// free name with a numeric suffix (github-2, then github-3), recorded in the returned connections'
// SecretRefs (cloned, never stored), so every name the conversion writes (SecretName) is the
// new one. Each rename that creates a secret is logged (secret renamed), naming the connection
// and both names, never a value; a second run finds its own suffixed secret and creates nothing.
func (m *Mover) secrets(ctx context.Context, conns []domain.ResolvedConnection) ([]domain.ResolvedConnection, int, error) {
	out := slices.Clone(conns)
	n := 0

	for _, s := range movedSecrets(conns) {
		name, created, err := m.place(ctx, s.secret)
		if err != nil {
			return nil, n, err
		}

		if created {
			n++
		}

		if name == s.secret.Name {
			continue
		}

		rc := &out[s.conn]

		if created {
			m.d.Logger.WarnContext(ctx, "secret renamed", slog.String("connection", rc.Connection.Name), slog.String("field", s.field),
				slog.String("from", s.secret.Name), slog.String("to", name),
				slog.String("reason", "a secret named "+s.secret.Name+" holds another value or is open to some factories only"))
		}

		refs := maps.Clone(rc.Connection.SecretRefs)
		if refs == nil {
			refs = map[string]string{}
		}

		refs[s.field] = name
		rc.Connection.SecretRefs = refs
	}

	return out, n, nil
}

// maxTries bounds the names place tries for one secret.
const maxTries = 100

// place is the name of the secret s becomes, and whether it created it: the first of s's names
// (suffixed) that is free, where it creates s, or that holds s's value open to every factory.
func (m *Mover) place(ctx context.Context, s domain.NewSecret) (string, bool, error) {
	for i := 1; i <= maxTries; i++ {
		name := suffixed(s.Name, i)

		same, err := m.holds(ctx, name, s.Value)

		switch {
		case errors.Is(err, ports.ErrNotFound):
			next := s
			next.Name = name

			if _, err := m.d.Secrets.Create(ctx, next, Actor); errors.Is(err, ports.ErrNameTaken) {
				continue
			} else if err != nil {
				return "", false, fmt.Errorf("create secret %s: %w", name, err)
			}

			return name, true, nil
		case err != nil:
			return "", false, err
		case same:
			return name, false, nil
		}
	}

	return "", false, fmt.Errorf("secret %s: no free name among %d tries", s.Name, maxTries)
}

// holds reports whether secret name is open to every factory and holds value; ports.ErrNotFound
// when there is no such secret. A secret with another scope is not opened: the conversion makes
// every secret open to all, so it is not one of its own.
func (m *Mover) holds(ctx context.Context, name, value string) (bool, error) {
	sec, err := m.d.Secrets.Get(ctx, name)

	switch {
	case errors.Is(err, ports.ErrNotFound):
		return false, err
	case err != nil:
		return false, fmt.Errorf("read secret %s: %w", name, err)
	case !sec.Scope.All:
		return false, nil
	}

	opened, err := m.d.Secrets.Resolve(ctx, []string{name}, domain.SecretUse{})
	if err != nil {
		return false, fmt.Errorf("open secret %s: %w", name, err)
	}

	return len(opened) == 1 && subtle.ConstantTimeCompare([]byte(opened[0].Value), []byte(value)) == 1, nil
}

// sources points each synced knowledge source that reads a connection and no secret at the
// secret its connection's token became (SecretName), with its reader, its code host and the
// connection's other set values in its config, which keeps its own keys; it keeps the connection
// until connections go. A source it cannot map is logged (knowledge source not moved over) with
// the reason, never a value, and keeps reading its connection. It counts the sources it changed;
// a second run finds none.
func (m *Mover) sources(ctx context.Context, conns []domain.ResolvedConnection) (int, error) {
	list, err := m.d.Knowledge.Sources(ctx)
	if err != nil {
		return 0, fmt.Errorf("list knowledge sources: %w", err)
	}

	n := 0

	for _, src := range list {
		if src.Kind != domain.SourceSynced || src.Connection == "" || src.Secret != "" {
			continue
		}

		p, reason, err := m.sourcePatch(ctx, src, conns)
		if err != nil {
			return n, err
		}

		if reason != "" {
			m.d.Logger.WarnContext(ctx, "knowledge source not moved over", slog.String("source", src.Name),
				slog.String("connection", src.Connection), slog.String("reason", reason))

			continue
		}

		if _, err := m.d.Knowledge.UpdateSource(ctx, src.Name, p, Actor); err != nil {
			return n, fmt.Errorf("point knowledge source %s at its secret: %w", src.Name, err)
		}

		n++
	}

	return n, nil
}

// sourcePatch is what points src at its connection's secret, or why it cannot ("" when it can):
// among the reasons, a secret open to some factories only, which the ingestor cannot read with.
func (m *Mover) sourcePatch(ctx context.Context, src domain.KnowledgeSource, conns []domain.ResolvedConnection) (domain.KnowledgeSourcePatch, string, error) {
	i := slices.IndexFunc(conns, func(rc domain.ResolvedConnection) bool { return rc.Connection.Name == src.Connection })
	if i < 0 {
		return domain.KnowledgeSourcePatch{}, "its connection was not found", nil
	}

	rc := conns[i]

	if rc.Kind.Spec.Sync == "" {
		return domain.KnowledgeSourcePatch{}, "its connection's kind " + rc.Kind.Name + " names no reader", nil
	}

	field, ok := tokenField(rc.Kind)
	if !ok {
		return domain.KnowledgeSourcePatch{}, "its connection's kind " + rc.Kind.Name + " has no one secret field that holds its token", nil
	}

	name := SecretName(rc.Connection, rc.Kind, field)

	sec, err := m.d.Secrets.Get(ctx, name)

	switch {
	case errors.Is(err, ports.ErrNotFound):
		return domain.KnowledgeSourcePatch{}, "there is no secret " + name, nil
	case err != nil:
		return domain.KnowledgeSourcePatch{}, "", fmt.Errorf("read secret %s: %w", name, err)
	case !sec.Scope.All:
		// The ingestor opens a source's secret for no factory (ErrSecretNotAllowed).
		return domain.KnowledgeSourcePatch{}, "secret " + name + " is open to some factories only; a knowledge source reads with a secret open to all", nil
	}

	config := maps.Clone(src.Config)
	if config == nil {
		config = map[string]string{}
	}

	config[domain.SourceReaderKey] = rc.Kind.Spec.Sync

	if rc.Kind.Spec.CodeHost != "" {
		config["code_host"] = rc.Kind.Spec.CodeHost
	}

	for _, f := range rc.Kind.Spec.Fields {
		if _, set := config[f.Name]; !f.Secret && !set && rc.Values[f.Name] != "" {
			config[f.Name] = rc.Values[f.Name]
		}
	}

	return domain.KnowledgeSourcePatch{Secret: &name, Config: config}, "", nil
}

// tokenField is the secret field of k that holds the token a reader reads with: its one secret
// field, or the one named token among several.
func tokenField(k domain.ConnectionKind) (string, bool) {
	fields := secretFields(k)

	switch {
	case len(fields) == 1:
		return fields[0], true
	case slices.Contains(fields, "token"):
		return "token", true
	default:
		return "", false
	}
}

// files reads version v's tree.
func (m *Mover) files(ctx context.Context, v domain.DefinitionVersion) (map[string][]byte, error) {
	out := make(map[string][]byte, len(v.Manifest))

	for _, p := range slices.Sorted(maps.Keys(v.Manifest)) {
		b, err := m.d.Definitions.Blob(ctx, v.Manifest[p])
		if err != nil {
			return nil, fmt.Errorf("read %s of version %s: %w", p, v.ID, err)
		}

		out[p] = b
	}

	return out, nil
}

// factory converts rec's current version and returns its conversion: nil for a version on
// secrets already, refused with the problems for one that cannot convert, fails its tests (check)
// or fails its publish checks. Once the converted version is published, it re-keys the
// organization's standing rules onto its environments (rekey).
func (m *Mover) factory(ctx context.Context, rec ports.FactoryRecord, conns []domain.ResolvedConnection) (*ports.FactoryConversion, error) {
	current, err := m.d.Definitions.Version(ctx, rec.Current.ID)
	if err != nil {
		return nil, fmt.Errorf("read version %s: %w", rec.Current.ID, err)
	}

	tree, err := m.files(ctx, current)
	if err != nil {
		return nil, err
	}

	r := Convert(tree, conns)
	if r.OnSecrets {
		return nil, nil
	}

	c := &ports.FactoryConversion{Factory: rec.ID, From: current.ID, Names: r.Names, At: m.d.Clock.Now()}
	if len(r.Problems) > 0 {
		c.Problems = r.Problems

		return c, nil
	}

	if c.Problems, err = m.check(ctx, rec.ID, r.Files); err != nil || len(c.Problems) > 0 {
		return c, err
	}

	open, err := m.d.Definitions.Draft(ctx, rec.ID)

	switch {
	case errors.Is(err, ports.ErrNotFound):
		open = domain.DefinitionVersion{}
	case err != nil:
		return nil, fmt.Errorf("read draft: %w", err)
	}

	v, pubErr := m.publish(ctx, rec, r.Files)
	if pubErr != nil {
		c.Problems = []string{fmt.Sprintf("the converted version does not publish: %v", pubErr)}
	} else {
		c.To = v.ID

		if err := m.rekey(ctx, rec.ID, r.Servers); err != nil {
			return nil, err
		}
	}

	if err := m.reopen(ctx, rec.ID, open, cmpOr(v.ID, current.ID), conns); err != nil {
		return nil, err
	}

	return c, nil
}

// rekey copies each active standing rule keyed by a connection onto every environment of factory
// f whose converted file has a server named after the connection (servers, Result.Servers): the
// same tool and agent role, made by Goal with the item the rule was approved on (ADR 0037
// decision 19, step 4). The rules stay as they are. A copy made before is found instead of made
// again (ports.Connections.CreateServerStandingRule), so re-keying twice copies nothing.
func (m *Mover) rekey(ctx context.Context, f domain.FactoryID, servers map[string][]string) error {
	rules, err := m.d.Connections.StandingRules(ctx, false)
	if err != nil {
		return fmt.Errorf("list standing rules: %w", err)
	}

	for _, r := range rules {
		if r.ConnectionID == "" || r.Connection == "" {
			continue
		}

		for _, env := range slices.Sorted(maps.Keys(servers)) {
			if !slices.Contains(servers[env], r.Connection) {
				continue
			}

			n := domain.NewServerStandingRule{Factory: f, Environment: env, Server: r.Connection, Tool: r.Tool, Agent: r.Agent, Item: r.ItemID}
			if _, err := m.d.Connections.CreateServerStandingRule(ctx, n, Actor); err != nil {
				return fmt.Errorf("re-key standing rule %s onto environment %s: %w", r.ID, env, err)
			}
		}
	}

	return nil
}

func cmpOr(a, b domain.DefinitionVersionID) domain.DefinitionVersionID {
	if a != "" {
		return a
	}

	return b
}

// publish opens a draft of Goal's on the current version, saves the converted tree in it and
// publishes it with the publish checks.
func (m *Mover) publish(ctx context.Context, rec ports.FactoryRecord, tree map[string][]byte) (domain.DefinitionVersion, error) {
	d, _, err := m.d.Definitions.OpenDraft(ctx, ports.OpenDraft{Factory: rec.ID, Base: rec.Current.ID, Replace: true, By: Actor})
	if err != nil {
		return domain.DefinitionVersion{}, fmt.Errorf("open draft: %w", err)
	}

	manifest, err := m.d.Definitions.PutBlobs(ctx, tree)
	if err != nil {
		return domain.DefinitionVersion{}, fmt.Errorf("store files: %w", err)
	}

	d, err = m.d.Definitions.SaveDraft(ctx, ports.DraftChange{Factory: rec.ID, ExpectedRevision: d.Revision, Set: manifest, Replace: true, By: Actor})
	if err != nil {
		return domain.DefinitionVersion{}, fmt.Errorf("save draft: %w", err)
	}

	return m.d.Publisher.Publish(ctx, rec.ID, authoring.PublishRequest{Revision: d.Revision, ExpectedCurrent: rec.Current.ID, Message: Message})
}

// reopen puts back the draft open before the conversion, on base, converted when it converts and
// as it was when it does not, saved by its own author; without one it discards the draft Goal
// left after a publish that failed.
func (m *Mover) reopen(ctx context.Context, f domain.FactoryID, open domain.DefinitionVersion, base domain.DefinitionVersionID,
	conns []domain.ResolvedConnection,
) error {
	if open.ID == "" {
		d, err := m.d.Definitions.Draft(ctx, f)

		switch {
		case errors.Is(err, ports.ErrNotFound):
			return nil
		case err != nil:
			return fmt.Errorf("read draft: %w", err)
		}

		if err := m.d.Definitions.DiscardDraft(ctx, f, d.Revision, Actor); err != nil {
			return fmt.Errorf("discard draft: %w", err)
		}

		return nil
	}

	tree, err := m.files(ctx, open)
	if err != nil {
		return err
	}

	if r := Convert(tree, conns); r.Files != nil {
		tree = r.Files
	}

	author := open.UpdatedBy
	if author == (domain.Actor{}) {
		author = open.CreatedBy
	}

	d, _, err := m.d.Definitions.OpenDraft(ctx, ports.OpenDraft{Factory: f, Base: base, Replace: true, By: author})
	if err != nil {
		return fmt.Errorf("reopen draft: %w", err)
	}

	manifest, err := m.d.Definitions.PutBlobs(ctx, tree)
	if err != nil {
		return fmt.Errorf("store draft files: %w", err)
	}

	if _, err := m.d.Definitions.SaveDraft(ctx, ports.DraftChange{Factory: f, ExpectedRevision: d.Revision, Set: manifest, Replace: true, By: author}); err != nil {
		return fmt.Errorf("save reopened draft: %w", err)
	}

	return nil
}

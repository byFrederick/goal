package moveover

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing/fstest"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
)

// sourceCodeHostKey is the config key of a repository source naming its code host's kind (github
// or gitlab) for a host other than github.com and gitlab.com, as gitdocs reads it and sourcePatch
// writes it.
const sourceCodeHostKey = "code_host"

// versionsPage is how many published versions of a factory codeHosts reads at once.
const versionsPage = 100

// usedHost is a code host the organization already sends a token or a secret to, and where it
// was found, for the logs.
type usedHost struct {
	host string
	kind domain.CodeHostKind
	from string
}

// codeHosts seeds the organization's code hosts, once (ADR 0041 decision 19), so no token stops
// being sent once goald sends them only to listed hosts: the host of every published version's
// runtime.repository, with its forge, then the host of every knowledge repository source, with
// its code_host config, github.com and gitlab.com left out. A host it cannot list (no DNS name,
// a port, no kind, or a second kind) is logged (code host not seeded) with its host and the
// reason, never a token. It counts the hosts it added in t; after the first seed, it adds none.
func (m *Mover) codeHosts(ctx context.Context, t *Tally) error {
	used, err := m.versionHosts(ctx)
	if err != nil {
		return err
	}

	srcs, err := m.d.Knowledge.Sources(ctx)
	if err != nil {
		return fmt.Errorf("list knowledge sources: %w", err)
	}

	for _, src := range srcs {
		if h, ok := sourceHost(src); ok {
			used = append(used, h)
		}
	}

	hosts := m.listable(ctx, used)

	before, err := m.d.Hosts.CodeHosts(ctx)
	if err != nil {
		return fmt.Errorf("read code hosts: %w", err)
	}

	seeded, err := m.d.Hosts.SeedCodeHosts(ctx, Actor, hosts)
	if err != nil {
		return fmt.Errorf("seed code hosts: %w", err)
	}

	if seeded {
		for _, h := range hosts {
			if !slices.ContainsFunc(before, func(b domain.CodeHost) bool { return b.Host == h.Host }) {
				t.Hosts++
			}
		}
	}

	return nil
}

// versionHosts are the hosts of the runtime.repository of every published version of every
// factory, factory by factory, newest version first. Each factory.yaml is read once, however
// many versions share it; one that does not parse names no host.
func (m *Mover) versionHosts(ctx context.Context) ([]usedHost, error) {
	recs, err := m.d.Definitions.FactoryRecords(ctx)
	if err != nil {
		return nil, fmt.Errorf("list factories: %w", err)
	}

	var out []usedHost

	read := map[domain.BlobHash]bool{}

	for _, rec := range recs {
		for before := 0; ; {
			vs, err := m.d.Definitions.Versions(ctx, rec.ID, before, versionsPage)
			if err != nil {
				return nil, fmt.Errorf("list versions of factory %s: %w", rec.ID, err)
			}

			for _, v := range vs {
				h, ok := v.Manifest[config.FileName]
				if !ok || read[h] {
					continue
				}

				read[h] = true

				b, err := m.d.Definitions.Blob(ctx, h)
				if err != nil {
					return nil, fmt.Errorf("read %s of version %s: %w", config.FileName, v.ID, err)
				}

				if u, ok := repositoryHost(b); ok {
					u.from = "factory " + string(rec.ID) + " version " + strconv.Itoa(v.Number)
					out = append(out, u)
				}
			}

			if len(vs) < versionsPage {
				break
			}

			before = vs[len(vs)-1].Number
		}
	}

	return out, nil
}

// repositoryHost is the host and forge of the runtime.repository of factory.yaml raw.
func repositoryHost(raw []byte) (usedHost, bool) {
	f, _ := config.Check(fstest.MapFS{config.FileName: &fstest.MapFile{Data: raw}})
	if f == nil || f.Spec.Runtime.Repository == nil {
		return usedHost{}, false
	}

	r := f.Spec.Runtime.Repository

	host, forge := domain.Repository{URL: r.URL, Forge: r.Forge}.CodeHost()
	if host == "" {
		return usedHost{}, false
	}

	return usedHost{host: host, kind: domain.CodeHostKind(forge)}, true
}

// sourceHost is the host of a repository source, or of a synced one, whose config names a
// repository URL, with the kind gitdocs reads it as: github.com and gitlab.com by name, any other
// host by its code_host config.
func sourceHost(src domain.KnowledgeSource) (usedHost, bool) {
	if src.Kind != domain.SourceRepository && src.Kind != domain.SourceSynced {
		return usedHost{}, false
	}

	u, err := url.Parse(strings.TrimSpace(src.Config[domain.SourceRepositoryKey]))
	if err != nil || u.Host == "" {
		return usedHost{}, false
	}

	host := strings.ToLower(u.Host)
	kind := cmp.Or(strings.TrimSpace(src.Config[sourceCodeHostKey]), domain.KnownForge(host))

	return usedHost{host: host, kind: domain.CodeHostKind(kind), from: "knowledge source " + src.Name}, true
}

// listable are the hosts of used that can be listed, each once with the first kind found; the
// built-in hosts are left out, and every other one that cannot be listed is logged once.
func (m *Mover) listable(ctx context.Context, used []usedHost) []domain.CodeHost {
	var out []domain.CodeHost

	first := map[string]domain.CodeHostKind{}

	for _, u := range used {
		if kind, seen := first[u.host]; seen {
			if kind != u.kind && kind != "" {
				m.notSeeded(ctx, u, fmt.Sprintf("%s names it as %s, and the kind found first, %s, is kept", u.from, cmp.Or(string(u.kind), "no kind"), kind))
			}

			continue
		}

		first[u.host] = u.kind

		if u.kind == "" {
			m.notSeeded(ctx, u, u.from+" names no kind of code host (github or gitlab) for it")

			continue
		}

		h, err := domain.CheckCodeHost(u.host, u.kind)

		switch {
		case errors.Is(err, domain.ErrCodeHostBuiltIn):
			continue
		case err != nil:
			m.notSeeded(ctx, u, u.from+": "+err.Error())

			continue
		}

		out = append(out, h)
	}

	return out
}

// notSeeded logs a host the seed leaves out, with why; the reason names the host, never a token.
func (m *Mover) notSeeded(ctx context.Context, u usedHost, reason string) {
	m.d.Logger.WarnContext(ctx, "code host not seeded", slog.String("host", u.host), slog.String("reason", reason))
}

// Connections before phase 4 (D §10.8, canvas Define-Connections and New-Connections): the
// draft's connections with their kind, how each signs in and its status from B's problems; the
// chosen one's agents with access and Remove…; and, until the connector catalog arrives
// (`meta.capabilities.connectors`), a placeholder that says to add connections in YAML. No tools
// table, health or test call: phase 4's `shared/connections-editor` replaces the detail panel.
import { useQuery } from '@tanstack/react-query';
import { ChevronLeft, Plug } from 'lucide-react';
import { builderMetaQuery } from '../../../api/queries';
import { cn } from '../../../ui/cn';
import { EmptyState } from '../../../ui/empty-state';
import { useDraft, useDraftSession, useSection, useViewOnly } from '../draft/use-draft';
import { bareSection, SectionShell } from '../frame/section-shell';
import { useValidation } from '../frame/use-editor';
import { SearchLink } from '../search-link';
import type { SectionViewProps } from '../section-views';
import { ConnectionDetail } from './connection-detail';
import { connectionKind, connectionStatus, signIn, statusTone } from './describe';

/** Two letters for the entry's square: the initials of a two-word name, else its first two letters. */
const monogram = (name: string) => {
  const words = name.split(/[-_]/).filter(Boolean);
  return (words.length > 1 ? `${words[0]?.[0] ?? ''}${words[1]?.[0] ?? ''}` : name.slice(0, 2)).toLowerCase();
};

export function ConnectionsSection({ search }: SectionViewProps) {
  const session = useDraftSession();
  const viewOnly = useViewOnly();
  const connections = useSection('connections');
  const factoryName = useSection('basics').name;
  const problems = useValidation()?.problems ?? [];
  const chosen = search.connection ? connections.find((c) => c.name === search.connection) : connections[0];
  const index = chosen ? connections.indexOf(chosen) : -1;
  const meta = useQuery(builderMetaQuery(session.org)).data;

  return (
    <SectionShell section="connections" className={bareSection}>
      <div className="grid items-start gap-4 @[760px]:grid-cols-[330px_minmax(0,1fr)]">
        <div className={cn('flex min-w-0 flex-col gap-4', search.connection && 'max-md:hidden')}>
          <nav
            aria-label="Connections"
            className="flex min-w-0 flex-col gap-2 rounded-card border border-line bg-card p-3.5"
          >
            <h3 translate="no" className="m-0 mx-1 mb-1 text-[15px] font-medium">
              Added to {factoryName ?? 'this factory'}
            </h3>
            {connections.length > 0 ? (
              <ul className="m-0 flex list-none flex-col gap-2 p-0">
                {connections.map((c, i) => {
                  const current = c === chosen;
                  const status = connectionStatus(c, i, problems);
                  return (
                    <li key={c.name}>
                      <SearchLink
                        patch={{ connection: c.name }}
                        aria-current={current ? 'true' : undefined}
                        className={cn(
                          'grid min-h-11 grid-cols-[36px_minmax(0,1fr)] items-center gap-x-3 gap-y-1 rounded-control border bg-surface p-3 text-ink no-underline transition-colors duration-200 @[420px]:grid-cols-[36px_minmax(0,1fr)_auto]',
                          current
                            ? 'border-ink shadow-[0_0_0_1px_var(--color-ink)]'
                            : 'border-line hover:border-rule-strong',
                        )}
                      >
                        <span
                          aria-hidden="true"
                          className="flex size-9 items-center justify-center rounded-control bg-control text-[13px] font-semibold"
                        >
                          {monogram(c.name)}
                        </span>
                        <span className="min-w-0">
                          <span translate="no" className="block truncate text-[15px] font-medium">
                            {c.name}
                          </span>
                          <span className="block text-[13px] text-muted">
                            {connectionKind(c)},{' '}
                            {signIn(c)
                              .replace(/^Uses /, '')
                              .replace(/^No sign-in$/, 'no sign-in')}
                          </span>
                        </span>
                        <span
                          className={cn(
                            'col-start-2 text-[13px] @[420px]:col-start-auto @[420px]:text-right',
                            statusTone[status.tone],
                          )}
                        >
                          {status.text}
                        </span>
                      </SearchLink>
                    </li>
                  );
                })}
              </ul>
            ) : (
              <p className="m-0 mx-1 text-sm text-muted">
                No connections. Agents can still read and edit the workspace.
              </p>
            )}
          </nav>
          {meta?.capabilities.connectors || viewOnly ? null : <CatalogPlaceholder />}
        </div>

        <div className={cn('flex min-w-0 flex-col gap-3', !search.connection && 'max-md:hidden')}>
          {search.connection ? (
            <SearchLink
              patch={{ connection: undefined }}
              className="inline-flex min-h-11 items-center gap-1 self-start text-[15px] text-ink md:hidden"
            >
              <ChevronLeft size={18} strokeWidth={1.5} aria-hidden="true" />
              All connections
            </SearchLink>
          ) : null}
          {chosen ? (
            <ConnectionDetail key={chosen.name} connection={chosen} index={index} />
          ) : connections.length === 0 ? null : (
            <EmptyState title={`No connection named ${search.connection}`}>
              It may have been removed. Pick one from the list.
            </EmptyState>
          )}
        </div>
      </div>
    </SectionShell>
  );
}

/** "Add from the catalog" until the connector catalog arrives: connections are added in YAML. */
function CatalogPlaceholder() {
  const line = useDraft((s) => s.ranges['/connections']?.[0]);
  return (
    <section
      aria-labelledby="catalog-placeholder"
      className="flex min-w-0 flex-col items-start gap-2.5 rounded-card border border-line bg-card p-3.5"
    >
      <span
        aria-hidden="true"
        className="flex size-9 items-center justify-center rounded-control border border-rule bg-surface text-ink-soft"
      >
        <Plug size={18} strokeWidth={1.5} />
      </span>
      <h3 id="catalog-placeholder" className="m-0 text-[15px] font-medium">
        Add from the catalog
      </h3>
      <p className="m-0 text-sm leading-normal text-muted">
        Connectors arrive with the connector catalog. Until then, add a connection in YAML.
      </p>
      <SearchLink
        patch={{ mode: 'yaml', line, connection: undefined }}
        className="inline-flex min-h-11 items-center text-sm text-ink underline underline-offset-3 hover:text-run md:min-h-8"
      >
        Open YAML at connections
      </SearchLink>
    </section>
  );
}

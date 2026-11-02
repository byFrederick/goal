// Connections (canvas "Connections" in docs/design/screens/connections/connections.jpg): the
// organization's accounts and keys, grouped by what they are (code hosts, apps, MCP servers and model
// providers, from the kind), each with its status, what uses it and when it was last used. Three
// summary tiles, and a failing test is called out above the list with Replace secret…. The search text,
// kind and factory live in the URL; ?factory= asks goald for what that factory's current version names.
// Owners and admins (connection:manage) get Add connection, Test and Replace secret…; members read.
// The search lives in the top bar; kind and factory narrow the list only through the URL.
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { Ellipsis, KeyRound, Plug, TriangleAlert } from 'lucide-react';
import { type ReactNode, useDeferredValue, useState } from 'react';
import { connectionKindsQuery, connectionsQuery, factoriesQuery } from '../../api/queries';
import type { Connection, ConnectionKind, ConnectionKindField } from '../../api/types';
import { FactoryBadge } from '../../app/factory-badge';
import { TopBarSlot } from '../../app/top-bar';
import { namesText, runEnv, since, usedByText } from '../../shared/connections';
import { factoryMark } from '../../shared/marks';
import { useOrg, useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { ConnectionGlyph } from '../../ui/connection-glyph';
import { EmptyState } from '../../ui/empty-state';
import { Menu, MenuContent, MenuItem, MenuTrigger } from '../../ui/menu';
import { NumberRoll } from '../../ui/number-roll';
import { SearchField } from '../../ui/search-field';
import { Skeleton } from '../../ui/skeleton';
import { StatusDot } from './parts';
import { ReplaceSecretDialog } from './replace-secret-dialog';
import { useTestConnection } from './use-test';

type GroupKey = 'code_host' | 'apps' | 'mcp' | 'model';

/** The groups in the order the screen shows them, with the line each one says under its heading. */
const groups: { key: GroupKey; title: string; description: string }[] = [
  { key: 'code_host', title: 'Code hosts', description: 'Where factories read and change code' },
  { key: 'apps', title: 'Apps', description: 'Where work comes from and where agents report' },
  { key: 'mcp', title: 'MCP servers', description: 'Extra tools for agents' },
  { key: 'model', title: 'Model providers', description: 'Who pays for the agents’ thinking' },
];

/** The group a connection belongs to, from its kind: model, then code host, then MCP, otherwise apps. */
function groupOf(kind: ConnectionKind | undefined): GroupKey {
  if (kind?.model) return 'model';
  if (kind?.code_host) return 'code_host';
  if (kind?.modes.includes('mcp')) return 'mcp';
  return 'apps';
}

/** A connection needs attention when its test failed or its secret is missing. */
const needsAttention = (c: Connection) => c.status === 'failing' || c.status === 'missing-secret';

// The stat strip of the export: three plain figures divided by a hairline, no cards.
const tileClass =
  'flex min-w-0 w-full flex-col gap-1 border-line py-2 text-left @[640px]:border-l @[640px]:pl-6 first:border-l-0 first:pl-0';

export function ConnectionsPage() {
  const org = useOrg();
  const search = useSearch({ from: '/o/$org/_org/connections/' });
  const navigate = useNavigate({ from: '/o/$org/connections/' });
  const manage = useOrgHolds('connection:manage');
  const listed = useQuery(factoriesQuery(org.id));
  const factories = listed.data ?? [];
  const factory = search.factory ? factories.find((f) => f.name === search.factory) : undefined;
  // A ?factory= the organization has no factory of (a typo, a renamed factory) is said, never unfiltered.
  const missing = search.factory !== undefined && listed.isSuccess && !factory;
  const list = useQuery({
    ...connectionsQuery(org.id, factory ? { factory: factory.id } : {}),
    enabled: !search.factory || factory !== undefined,
  });
  const kinds = useQuery(connectionKindsQuery(org.id)).data ?? [];
  const kindOf = (c: Connection) => kinds.find((k) => k.name === c.kind);
  const tester = useTestConnection(org.id);
  const [replacing, setReplacing] = useState<{ connection: Connection; field: ConnectionKindField } | null>(null);
  const [text, setText] = useState(search.q ?? '');
  const q = useDeferredValue(text.trim().toLowerCase());
  const now = Date.now();

  const all = missing ? [] : (list.data ?? []);
  const shown = all.filter(
    (c) =>
      (!search.kind || c.kind === search.kind) &&
      (!q || [c.name, c.description, kindOf(c)?.title ?? c.kind].some((s) => s.toLowerCase().includes(q))),
  );
  const failing = all.filter((c) => c.status === 'failing');

  const setSearch = (patch: Partial<typeof search>) =>
    void navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true });

  /** Replace secret…: its one secret field's dialog; a kind with several opens the connection. */
  const replaceSecret = (c: Connection) => {
    const secrets = kindOf(c)?.fields.filter((f) => f.secret) ?? [];
    const only = secrets.length === 1 ? secrets[0] : undefined;
    if (only) setReplacing({ connection: c, field: only });
    else void navigate({ to: '/o/$org/connections/$connection', params: { org: org.slug, connection: c.name } });
  };

  const searchField = (
    <SearchField
      value={text}
      label="Search connections"
      className="w-36 md:w-56"
      onChange={(value) => {
        setText(value);
        setSearch({ q: value.trim() || undefined });
      }}
    />
  );

  const addConnection = manage ? (
    <Button asChild variant="primary" className="h-11 md:h-8.5">
      <Link to="/o/$org/connections/new" params={{ org: org.slug }}>
        <span aria-hidden="true">+</span> Add connection
      </Link>
    </Button>
  ) : null;

  return (
    <>
      <TopBarSlot
        crumb="Connections"
        actions={
          <>
            {searchField}
            {addConnection}
          </>
        }
      />
      <header className="flex flex-col gap-1">
        <h1 className="m-0 text-page-title">Connections</h1>
        <p className="m-0 text-body text-muted">What Goal’s agents can reach, for every factory in Goal.</p>
      </header>

      {failing.map((c) => (
        <section
          key={c.id}
          aria-label={`${c.name} failed its test`}
          className="flex flex-col gap-3 rounded-card bg-rework-tint p-4 @[640px]:flex-row @[640px]:items-center @[640px]:justify-between"
        >
          <p className="m-0 flex items-start gap-3 text-body text-rework-ink">
            <TriangleAlert size={18} strokeWidth={1.75} aria-hidden="true" className="shrink-0" />
            <span>
              <strong className="font-control">{c.name} failed its test.</strong>{' '}
              {c.last_test?.error ? `${c.last_test.error}. ` : ''}
              {c.used_by.factories.length > 0
                ? `${namesText(c.used_by.factories.map((f) => f.name))}’s runs still get ${
                    runEnv(kindOf(c), c)[0] ?? 'it'
                  }, and their calls fail.`
                : 'Nothing uses it yet.'}
            </span>
          </p>
          {manage ? (
            <Button variant="white" className="h-11 md:h-8.5 shrink-0" onClick={() => replaceSecret(c)}>
              Replace secret…
            </Button>
          ) : null}
        </section>
      ))}

      {list.isPending && !missing ? (
        <Skeleton
          ready={false}
          placeholder={
            <div className="flex flex-col gap-3 rounded-card border border-line bg-card p-4">
              <div className="h-12 rounded-control bg-hover" />
              <div className="h-12 rounded-control bg-hover" />
              <div className="h-12 rounded-control bg-hover" />
            </div>
          }
        >
          <span />
        </Skeleton>
      ) : list.isError ? (
        <div
          className="flex max-w-xl flex-col items-start gap-3 rounded-card border border-line bg-card p-4"
          role="alert"
        >
          <Callout tone="rework">Couldn’t load connections. Check your connection and try again.</Callout>
          <Button variant="white" onClick={() => void list.refetch()}>
            Try again
          </Button>
        </div>
      ) : all.length === 0 && !search.factory ? (
        <EmptyState icon={<Plug size={20} strokeWidth={1.5} />} title="No connections yet" actions={addConnection}>
          {manage
            ? 'Add the GitHub token your agents use, then the model key their runs need.'
            : `Owners and admins of ${org.name} add connections.`}
        </EmptyState>
      ) : (
        <>
          <div className="grid grid-cols-1 gap-3 @[640px]:grid-cols-3 @[640px]:gap-6">
            <div className={cn(tileClass, 'justify-between')}>
              <SummaryLabel>Connected</SummaryLabel>
              <SummaryValue value={all.filter((c) => c.status === 'ok').length} />
            </div>
            <div className={cn(tileClass, 'justify-between')}>
              <SummaryLabel>Need attention</SummaryLabel>
              <SummaryValue value={all.filter(needsAttention).length} warn="text-rework-ink" />
            </div>
            <Link to="/o/$org/needs-you" params={{ org: org.slug }} className={cn(tileClass, 'justify-between')}>
              <SummaryLabel>Writes waiting for your OK</SummaryLabel>
              <SummaryValue value={all.reduce((sum, c) => sum + c.waiting_approvals, 0)} warn="text-you-ink" />
            </Link>
          </div>

          <div className="flex flex-col gap-3">
            {factory ? <p className="m-0 text-body text-muted">What {factory.name} uses</p> : null}

            {shown.length === 0 ? (
              <div className="flex flex-col items-start gap-2 rounded-card border border-line bg-card p-4">
                <p className="m-0 text-body text-muted">
                  {missing
                    ? `${org.name} has no factory named ${search.factory}.`
                    : search.factory && all.length === 0
                      ? `${search.factory}’s current version names no connection.`
                      : 'No connection matches this search.'}
                </p>
                <Button
                  variant="white"
                  onClick={() => {
                    setText('');
                    setSearch({ q: undefined, kind: undefined, factory: undefined });
                  }}
                >
                  Show every connection
                </Button>
              </div>
            ) : (
              groups.map((g) => {
                const rows = shown.filter((c) => groupOf(kindOf(c)) === g.key);
                if (rows.length === 0) return null;
                return (
                  <section key={g.key} aria-labelledby={`group-${g.key}`} className="flex flex-col gap-2">
                    <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                      <h2 id={`group-${g.key}`} className="m-0 text-heading">
                        {g.title}
                      </h2>
                      <p className="m-0 text-secondary text-muted">{g.description}</p>
                    </div>
                    <div className="overflow-hidden rounded-card border border-line bg-card">
                      <table className="hidden w-full border-collapse text-body @[960px]:table">
                        <caption className="sr-only">
                          {g.title} of {org.name}
                        </caption>
                        <thead>
                          <tr className="bg-panel text-left text-meta text-muted [&>th]:px-4 [&>th]:py-2 [&>th]:font-normal">
                            <th scope="col">Connection</th>
                            <th scope="col">Status</th>
                            <th scope="col">Used by</th>
                            <th scope="col">{g.key === 'model' ? 'Models' : 'Tools'}</th>
                            <th scope="col">Last used</th>
                            <th scope="col">
                              <span className="sr-only">Actions</span>
                            </th>
                          </tr>
                        </thead>
                        <tbody>
                          {rows.map((c) => {
                            const kind = kindOf(c);
                            return (
                              <tr
                                key={c.id}
                                className={cn(
                                  'border-t border-line align-top [&>td]:px-4 [&>td]:py-3',
                                  needsAttention(c) ? 'bg-rework-tint/40' : undefined,
                                )}
                              >
                                <td className="max-w-[260px]">
                                  <ConnectionName org={org.slug} connection={c} kind={kind} />
                                </td>
                                <td className="whitespace-nowrap">
                                  <StatusDot connection={c} kind={kind} />
                                </td>
                                <td className="max-w-[220px]">
                                  <UsedBy connection={c} factories={factories} />
                                </td>
                                <td className="whitespace-nowrap tabular-nums">
                                  {c.tools.length > 0 ? c.tools.length : <span className="text-muted">None</span>}
                                </td>
                                <td className="whitespace-nowrap text-secondary text-muted">
                                  {c.last_used_at ? since(c.last_used_at, now) : 'Never'}
                                </td>
                                <td className="whitespace-nowrap text-right">
                                  {manage && c.status === 'missing-secret' ? (
                                    <Button variant="white" className="mr-2 h-8.5" onClick={() => replaceSecret(c)}>
                                      Connect
                                    </Button>
                                  ) : null}
                                  <RowMenu
                                    org={org.slug}
                                    connection={c}
                                    kind={kind}
                                    manage={manage}
                                    onTest={() => tester.mutate(c.name)}
                                    onReplace={() => replaceSecret(c)}
                                  />
                                </td>
                              </tr>
                            );
                          })}
                        </tbody>
                      </table>

                      <ul
                        aria-label={`${g.title} of ${org.name}`}
                        className="m-0 flex list-none flex-col p-0 @[960px]:hidden"
                      >
                        {rows.map((c) => {
                          const kind = kindOf(c);
                          return (
                            <li key={c.id} className="flex flex-col gap-2 border-t border-line p-4 first:border-t-0">
                              <div className="flex items-start justify-between gap-3">
                                <ConnectionName org={org.slug} connection={c} kind={kind} />
                                <RowMenu
                                  org={org.slug}
                                  connection={c}
                                  kind={kind}
                                  manage={manage}
                                  onTest={() => tester.mutate(c.name)}
                                  onReplace={() => replaceSecret(c)}
                                />
                              </div>
                              <StatusDot connection={c} kind={kind} className="text-body" />
                              <p className="m-0 text-secondary text-muted">
                                {c.last_used_at ? `Used ${since(c.last_used_at, now).toLowerCase()}` : 'Never used'}
                                {c.used_by.factories.length > 0 ? `. Used by ${usedByText(c)}.` : ''}
                              </p>
                              {manage && needsAttention(c) ? (
                                <div className="flex flex-wrap gap-2">
                                  <Button variant="white" className="h-11" onClick={() => replaceSecret(c)}>
                                    {c.status === 'missing-secret' ? 'Set secret…' : 'Replace secret…'}
                                  </Button>
                                  {kind?.testable && c.status === 'failing' ? (
                                    <Button
                                      variant="white"
                                      className="h-11"
                                      disabled={tester.isPending && tester.variables === c.name}
                                      onClick={() => tester.mutate(c.name)}
                                    >
                                      {tester.isPending && tester.variables === c.name ? 'Testing…' : 'Test'}
                                    </Button>
                                  ) : null}
                                </div>
                              ) : null}
                            </li>
                          );
                        })}
                      </ul>
                    </div>
                  </section>
                );
              })
            )}
          </div>
        </>
      )}

      {replacing ? (
        <ReplaceSecretDialog
          org={org.id}
          connection={replacing.connection}
          field={replacing.field}
          onClose={() => setReplacing(null)}
        />
      ) : null}
    </>
  );
}

function SummaryLabel({ children }: { children: ReactNode }) {
  return <span className="text-secondary text-ink-soft">{children}</span>;
}

/** A figure: ink when it is zero, the warning colour only when something is waiting. */
function SummaryValue({ value, warn }: { value: number; warn?: string }) {
  return <NumberRoll value={value} className={cn('text-kpi text-ink', value > 0 && warn)} />;
}

function ConnectionName({
  org,
  connection: c,
  kind,
}: {
  org: string;
  connection: Connection;
  kind: ConnectionKind | undefined;
}) {
  return (
    <span className="flex min-w-0 items-start gap-3">
      <ConnectionGlyph kind={c.kind} title={kind?.title} />
      <span className="flex min-w-0 flex-col">
        <Link
          to="/o/$org/connections/$connection"
          params={{ org, connection: c.name }}
          translate="no"
          className="truncate font-control text-body text-ink underline-offset-3 hover:underline"
        >
          {c.name}
        </Link>
        <span className="truncate text-secondary text-muted">{c.description || kind?.title || c.kind}</span>
      </span>
    </span>
  );
}

function UsedBy({ connection: c, factories }: { connection: Connection; factories: readonly { name: string }[] }) {
  if (c.used_by.factories.length === 0 && c.used_by.knowledge_sources.length === 0) {
    return <span className="text-muted">Nothing yet</span>;
  }
  return (
    <span className="flex flex-wrap items-center gap-1">
      {c.used_by.factories.map((f) => (
        <span key={f.id} title={f.name} className="inline-flex">
          <FactoryBadge mark={factoryMark(f, factories.length > 0 ? factories : [f])} />
          <span className="sr-only">{f.name}</span>
        </span>
      ))}
      {c.used_by.knowledge_sources.map((s) => (
        <Chip key={s} className="border-dashed font-normal">
          <span className="sr-only">knowledge source </span>
          {s}
        </Chip>
      ))}
    </span>
  );
}

function RowMenu({
  org,
  connection: c,
  kind,
  manage,
  onTest,
  onReplace,
}: {
  org: string;
  connection: Connection;
  kind: ConnectionKind | undefined;
  manage: boolean;
  onTest: () => void;
  onReplace: () => void;
}) {
  const hasSecret = kind?.fields.some((f) => f.secret) ?? false;
  return (
    <Menu>
      <MenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          className="size-11 text-muted md:size-8"
          aria-label={`Actions for ${c.name}`}
        >
          <Ellipsis size={18} strokeWidth={1.5} aria-hidden="true" />
        </Button>
      </MenuTrigger>
      <MenuContent side="bottom" align="end" className="w-56">
        <MenuItem asChild>
          <Link to="/o/$org/connections/$connection" params={{ org, connection: c.name }}>
            Open
          </Link>
        </MenuItem>
        {manage && kind?.testable ? <MenuItem onSelect={onTest}>Test</MenuItem> : null}
        {manage && hasSecret ? (
          <MenuItem onSelect={onReplace}>
            <KeyRound size={16} strokeWidth={1.5} aria-hidden="true" className="text-muted" />
            {c.status === 'missing-secret' ? 'Set secret…' : 'Replace secret…'}
          </MenuItem>
        ) : null}
      </MenuContent>
    </Menu>
  );
}

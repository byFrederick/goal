// Connections (canvas "Connections" in docs/design/screens/connections/connections.jpg): the
// organization's accounts and keys, grouped by what they are (code hosts, apps, MCP servers and model
// providers, from the kind), each with its status, what uses it and when it was last used. Three
// summary tiles, and a failing test is called out above the list with Replace secret…. Filters (text,
// kind, factory) live in the URL; ?factory= asks goald for what that factory's current version names.
// Owners and admins (connection:manage) get Add connection, Test and Replace secret…; members read.
// Below, how runs get connections and, for those who read the audit log, the recent activity.
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { Ellipsis, KeyRound, Plug, Search, TriangleAlert } from 'lucide-react';
import { type ReactNode, useDeferredValue, useState } from 'react';
import { connectionActivityQuery, connectionKindsQuery, connectionsQuery, factoriesQuery } from '../../api/queries';
import type { Connection, ConnectionKind, ConnectionKindField } from '../../api/types';
import { TopBarSlot } from '../../app/top-bar';
import { namesText, runEnv, since, usedByText } from '../../shared/connections';
import { plural } from '../../shared/format';
import { useOrg, useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { EmptyState } from '../../ui/empty-state';
import { Field, Input, NativeSelect } from '../../ui/field';
import { Menu, MenuContent, MenuItem, MenuTrigger } from '../../ui/menu';
import { NotAvailable } from '../../ui/not-available';
import { Monogram, Status } from './parts';
import { ReplaceSecretDialog } from './replace-secret-dialog';
import { useTestConnection } from './use-test';
import { activityName, activityText, activityWhen } from './words';

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

const tileClass = 'flex min-w-0 w-full flex-col gap-2 rounded-card border border-line bg-card p-4 text-left';

export function ConnectionsPage() {
  const org = useOrg();
  const search = useSearch({ from: '/o/$org/_org/connections/' });
  const navigate = useNavigate({ from: '/o/$org/connections/' });
  const manage = useOrgHolds('connection:manage');
  const auditor = useOrgHolds('audit:read');
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
  const kindChoices = kinds.filter((k) => all.some((c) => c.kind === k.name));
  // The factory's own list is all there is to count; kind and text narrow it.
  const filtered = Boolean(search.kind || q);

  const setSearch = (patch: Partial<typeof search>) =>
    void navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true });

  /** Replace secret…: its one secret field's dialog; a kind with several opens the connection. */
  const replaceSecret = (c: Connection) => {
    const secrets = kindOf(c)?.fields.filter((f) => f.secret) ?? [];
    const only = secrets.length === 1 ? secrets[0] : undefined;
    if (only) setReplacing({ connection: c, field: only });
    else void navigate({ to: '/o/$org/connections/$connection', params: { org: org.slug, connection: c.name } });
  };

  const addConnection = manage ? (
    <Button asChild variant="primary" className="h-11 md:h-8.5">
      <Link to="/o/$org/connections/new" params={{ org: org.slug }}>
        <span aria-hidden="true">+</span> Add connection
      </Link>
    </Button>
  ) : null;

  return (
    <>
      <TopBarSlot crumb="Connections" actions={addConnection} />
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
        <div aria-busy="true" className="rounded-card border border-line bg-card p-4">
          <span role="status" className="sr-only">
            Loading connections
          </span>
        </div>
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
          <div className="grid grid-cols-1 gap-3 @[640px]:grid-cols-3">
            <div className={cn(tileClass, 'justify-between')}>
              <SummaryLabel>Connected</SummaryLabel>
              <SummaryValue>{all.filter((c) => c.status === 'ok').length}</SummaryValue>
            </div>
            <div className={cn(tileClass, 'justify-between')}>
              <SummaryLabel>Need attention</SummaryLabel>
              <SummaryValue className="text-rework-ink">{all.filter(needsAttention).length}</SummaryValue>
            </div>
            <NotAvailable label="Writes waiting for your OK">
              <button type="button" className={cn(tileClass, 'justify-between')}>
                <SummaryLabel>Writes waiting for your OK</SummaryLabel>
                <SummaryValue>–</SummaryValue>
              </button>
            </NotAvailable>
          </div>

          <div className="flex flex-col gap-3">
            <div className="flex flex-wrap items-end justify-between gap-3">
              <h2 className="m-0 text-heading">
                {filtered ? `${shown.length} of ${plural(all.length, 'connection')}` : plural(all.length, 'connection')}
                {factory ? <span className="text-muted"> {factory.name} uses</span> : null}
              </h2>
              <div className="grid w-full grid-cols-1 gap-2 @[640px]:w-auto @[640px]:grid-cols-[minmax(0,220px)_auto_auto]">
                <Field label={<span className="sr-only">Find a connection</span>}>
                  <span className="relative flex">
                    <Search
                      size={16}
                      strokeWidth={1.5}
                      aria-hidden="true"
                      className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-muted"
                    />
                    <Input
                      type="search"
                      value={text}
                      placeholder="Search connections"
                      onChange={(e) => {
                        setText(e.target.value);
                        setSearch({ q: e.target.value.trim() || undefined });
                      }}
                      className="pl-9"
                      autoComplete="off"
                    />
                  </span>
                </Field>
                <Field label={<span className="sr-only">Kind</span>}>
                  <NativeSelect
                    value={search.kind ?? ''}
                    onChange={(e) => setSearch({ kind: e.target.value || undefined })}
                  >
                    <option value="">All kinds</option>
                    {kindChoices.map((k) => (
                      <option key={k.name} value={k.name}>
                        {k.title}
                      </option>
                    ))}
                  </NativeSelect>
                </Field>
                <Field label={<span className="sr-only">Factory</span>}>
                  <NativeSelect
                    value={search.factory ?? ''}
                    onChange={(e) => setSearch({ factory: e.target.value || undefined })}
                  >
                    <option value="">All factories</option>
                    {missing ? <option value={search.factory}>{search.factory}</option> : null}
                    {factories.map((f) => (
                      <option key={f.id} value={f.name}>
                        {f.name}
                      </option>
                    ))}
                  </NativeSelect>
                </Field>
              </div>
            </div>

            {shown.length === 0 ? (
              <div className="flex flex-col items-start gap-2 rounded-card border border-line bg-card p-4">
                <p className="m-0 text-body text-muted">
                  {missing
                    ? `${org.name} has no factory named ${search.factory}.`
                    : search.factory && all.length === 0
                      ? `${search.factory}’s current version names no connection.`
                      : 'No connection matches these filters.'}
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
                                  <Status connection={c} kind={kind} />
                                </td>
                                <td className="max-w-[220px]">
                                  <UsedBy connection={c} />
                                </td>
                                <td className="whitespace-nowrap text-secondary text-muted">
                                  {c.last_used_at ? since(c.last_used_at, now) : 'Never'}
                                </td>
                                <td className="text-right">
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
                              <Status connection={c} kind={kind} className="text-body" />
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

      <div className="grid gap-4 @[960px]:grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)]">
        <HowRunsGetThem />
        {auditor ? <RecentActivity org={org.id} now={now} /> : null}
      </div>

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

function SummaryValue({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <span className={cn('text-kpi font-numeral tracking-numeral tabular-nums text-ink', className)}>{children}</span>
  );
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
      <Monogram name={kind?.title ?? c.kind} />
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

function UsedBy({ connection: c }: { connection: Connection }) {
  if (c.used_by.factories.length === 0 && c.used_by.knowledge_sources.length === 0) {
    return <span className="text-muted">Nothing yet</span>;
  }
  return (
    <span className="flex flex-wrap gap-1">
      {c.used_by.factories.map((f) => (
        <Chip key={f.id} tone="quiet" className="font-normal">
          {f.name}
        </Chip>
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
        <Button variant="white" size="icon" className="size-9" aria-label={`Actions for ${c.name}`}>
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

/** How a run gets its connections (spec 4 §4.1, §4.3), in four sentences. */
function HowRunsGetThem() {
  const rows = [
    'A run gets the connections its factory and agent use as environment variables the kind names (GH_TOKEN, JIRA_API_TOKEN), for APIs, CLIs and MCP servers started in its box.',
    'Remote MCP servers are called straight from the box, with headers taken from those variables.',
    'AWS: runs get short-lived credentials minted for them from the stored keys. Runs never see the keys.',
    'Secret values are write-only: replace them, never read them back. Members see names, kinds and status.',
  ];
  return (
    <section
      aria-labelledby="how-runs"
      className="flex min-w-0 flex-col gap-3 rounded-card border border-line bg-card p-5"
    >
      <h2 id="how-runs" className="m-0 text-heading">
        How runs get connections
      </h2>
      <ul className="m-0 flex list-none flex-col p-0">
        {rows.map((r) => (
          <li key={r} className="border-t border-line py-3 text-body first:border-t-0 first:pt-0 last:pb-0">
            {r}
          </li>
        ))}
      </ul>
    </section>
  );
}

function RecentActivity({ org, now }: { org: string; now: number }) {
  const activity = useQuery(connectionActivityQuery(org));
  const entries = activity.data?.entries ?? [];
  return (
    <section
      aria-labelledby="recent-activity"
      className="flex min-w-0 flex-col gap-3 rounded-card border border-line bg-card p-5"
    >
      <div className="flex items-baseline justify-between gap-3">
        <h2 id="recent-activity" className="m-0 text-heading">
          Recent activity
        </h2>
        <span className="text-secondary text-muted">From the audit log</span>
      </div>
      {activity.isPending ? (
        <p className="m-0 text-body text-muted">Loading…</p>
      ) : activity.isError ? (
        <p className="m-0 text-body text-muted">Couldn’t load the audit log.</p>
      ) : entries.length === 0 ? (
        <p className="m-0 text-body text-muted">Nothing yet.</p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {entries.map((e) => (
            <li
              key={e.id}
              className="flex items-start justify-between gap-3 border-t border-line py-3 first:border-t-0 first:pt-0 last:pb-0"
            >
              <span className="min-w-0 text-body">
                <span className="block break-words">
                  <span translate="no" className="font-control">
                    {activityName(e)}
                  </span>
                  : {activityText(e)}
                </span>
                <span className="block text-secondary text-muted">{e.actor.name}</span>
              </span>
              <span className="shrink-0 text-secondary text-muted">{activityWhen(e.at, now)}</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

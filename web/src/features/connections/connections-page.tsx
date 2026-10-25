// Connections (spec 4 §10, canvas "Connections" and "Mobile-Connections"): the organization's
// accounts and keys, each with its kind and modes, what uses it, its status and when it was last
// tested and used. A table when `main` is 960 px wide, cards below. Filters (text, kind, factory)
// live in the URL; ?factory= asks goald for what that factory's current version names. A failed
// test is called out above the list with Replace secret…. Owners and admins (connection:manage)
// get New connection, Test and Replace secret…; members read. Below, how runs get connections
// and, for those who read the audit log, the recent activity.
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { Ellipsis, KeyRound, Plug, Search, TriangleAlert } from 'lucide-react';
import { useDeferredValue, useState } from 'react';
import { connectionActivityQuery, connectionKindsQuery, connectionsQuery, factoriesQuery } from '../../api/queries';
import type { Connection, ConnectionKind, ConnectionKindField } from '../../api/types';
import { namesText, runEnv, testedText, usedByText, usedText } from '../../shared/connections';
import { plural } from '../../shared/format';
import { useOrg, useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { EmptyState } from '../../ui/empty-state';
import { Field, Input, NativeSelect } from '../../ui/field';
import { Menu, MenuContent, MenuItem, MenuTrigger } from '../../ui/menu';
import { descriptionClass, eyebrowClass, PageHeader } from '../../ui/page-header';
import { KindNotes, Modes, Monogram, Status } from './parts';
import { ReplaceSecretDialog } from './replace-secret-dialog';
import { useTestConnection } from './use-test';
import { activityName, activityText, activityWhen } from './words';

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

  const newConnection = manage ? (
    <Button asChild variant="primary" className="h-11 md:h-10">
      <Link to="/o/$org/connections/new" params={{ org: org.slug }}>
        <span aria-hidden="true">+</span> New connection
      </Link>
    </Button>
  ) : null;

  return (
    <>
      <PageHeader
        eyebrow={<p className={cn(eyebrowClass, 'md:hidden')}>{org.name}</p>}
        title="Connections"
        description={
          <>
            <p className={cn(descriptionClass, 'max-w-[760px] md:hidden')}>
              {org.name}’s accounts and keys. Runs get the ones their factory uses as environment variables.
            </p>
            <p className={cn(descriptionClass, 'hidden max-w-[760px] md:block')}>
              {org.name}’s accounts and keys for outside systems, model access included. Each factory’s definition names
              the connections it uses; its runs get them as environment variables.
            </p>
          </>
        }
        actions={newConnection}
      />

      {failing.map((c) => (
        <section
          key={c.id}
          aria-label={`${c.name} failed its test`}
          className="flex flex-col items-start gap-3 rounded-card border border-rework-line bg-rework-tint px-4 py-3.5 md:px-5"
        >
          <p className="m-0 flex items-start gap-2.5 text-[15px] leading-normal text-rework-ink">
            <TriangleAlert size={18} strokeWidth={1.75} aria-hidden="true" className="mt-0.5 shrink-0" />
            <span>
              <strong className="font-medium">{c.name} failed its test.</strong>{' '}
              {c.last_test?.error ? `${c.last_test.error}. ` : ''}
              {c.used_by.factories.length > 0
                ? `${namesText(c.used_by.factories.map((f) => f.name))}’s runs still get ${
                    runEnv(kindOf(c), c)[0] ?? 'it'
                  }, and their calls fail.`
                : 'Nothing uses it yet.'}
            </span>
          </p>
          {manage ? (
            <Button variant="white" className="h-11 md:h-9" onClick={() => replaceSecret(c)}>
              Replace secret…
            </Button>
          ) : null}
        </section>
      ))}

      {list.isPending && !missing ? (
        <div aria-busy="true" className="rounded-card border border-line bg-card p-5">
          <span role="status" className="sr-only">
            Loading connections
          </span>
          <div className="flex flex-col gap-4">
            {[0, 1, 2, 3].map((i) => (
              <div key={i} className="grid grid-cols-[36px_1fr_1fr] items-center gap-4">
                <div className="size-9 rounded-control bg-control" />
                <div className="h-3 rounded-chip bg-control" />
                <div className="h-3 rounded-chip bg-control" />
              </div>
            ))}
          </div>
        </div>
      ) : list.isError ? (
        <div
          className="flex max-w-xl flex-col items-start gap-3 rounded-card border border-line bg-card p-5"
          role="alert"
        >
          <Callout tone="rework">Couldn’t load connections. Check your connection and try again.</Callout>
          <Button variant="white" onClick={() => void list.refetch()}>
            Try again
          </Button>
        </div>
      ) : all.length === 0 && !search.factory ? (
        <EmptyState icon={<Plug size={20} strokeWidth={1.5} />} title="No connections yet" actions={newConnection}>
          {manage
            ? 'Add the GitHub token your agents use, then the model key their runs need.'
            : `Owners and admins of ${org.name} add connections.`}
        </EmptyState>
      ) : (
        <section aria-labelledby="connections-count" className="rounded-card border border-line bg-card p-4 md:p-5">
          <div className="mb-3 flex flex-wrap items-end justify-between gap-3">
            <h2 id="connections-count" className="m-0 text-lg font-medium">
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
                    placeholder="Find a connection…"
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
            <div className="flex flex-col items-start gap-2 border-t border-line pt-4">
              <p className="m-0 text-[15px] text-muted">
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
            <>
              <table className="hidden w-full border-collapse text-sm @[960px]:table">
                <caption className="sr-only">Connections of {org.name}</caption>
                <thead>
                  <tr className="text-left text-[13px] text-muted [&>th]:pb-2 [&>th]:font-normal">
                    <th scope="col">Connection</th>
                    <th scope="col">Kind and modes</th>
                    <th scope="col">Used by</th>
                    <th scope="col">Status, last test and use</th>
                    <th scope="col">
                      <span className="sr-only">Actions</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {shown.map((c) => {
                    const kind = kindOf(c);
                    return (
                      <tr
                        key={c.id}
                        className={cn(
                          'border-t border-line align-top [&>td]:py-3 [&>td]:pr-3',
                          c.status === 'failing' || c.status === 'missing-secret' ? 'bg-rework-tint/40' : undefined,
                        )}
                      >
                        <td className="max-w-[260px]">
                          <ConnectionName org={org.slug} connection={c} kind={kind} />
                        </td>
                        <td>
                          <span className="flex flex-col gap-1">
                            <span>{kind?.title ?? c.kind}</span>
                            <Modes kind={kind} />
                            <KindNotes kind={kind} />
                          </span>
                        </td>
                        <td className="max-w-[220px]">
                          <UsedBy connection={c} />
                        </td>
                        <td className="whitespace-nowrap">
                          <span className="flex flex-col gap-0.5">
                            <Status connection={c} kind={kind} />
                            <span className="text-[13px] text-muted">{testedText(c, now)}</span>
                            <span className="text-[13px] text-muted">{usedText(c, now)}</span>
                          </span>
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
                aria-label={`Connections of ${org.name}`}
                className="m-0 flex list-none flex-col gap-3 p-0 @[960px]:hidden"
              >
                {shown.map((c) => {
                  const kind = kindOf(c);
                  const trouble = c.status === 'failing' || c.status === 'missing-secret';
                  return (
                    <li
                      key={c.id}
                      className="flex flex-col gap-1.5 rounded-control border border-line bg-surface p-3.5"
                    >
                      <div className="flex items-baseline justify-between gap-3">
                        <Link
                          to="/o/$org/connections/$connection"
                          params={{ org: org.slug, connection: c.name }}
                          translate="no"
                          className="min-h-6 min-w-0 truncate font-mono text-[15px] font-medium text-ink underline-offset-3 hover:underline"
                        >
                          {c.name}
                        </Link>
                        <span className="shrink-0 text-[13px] text-muted">{kind?.title ?? c.kind}</span>
                      </div>
                      <Status connection={c} kind={kind} className="text-sm" />
                      <p className="m-0 text-[13px] text-muted">
                        {usedText(c, now)}.{c.used_by.factories.length > 0 ? ` Used by ${usedByText(c)}.` : ''}
                      </p>
                      {manage && trouble ? (
                        <div className="mt-1 flex flex-wrap gap-2">
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
            </>
          )}
        </section>
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
          className="truncate font-mono text-[15px] font-medium text-ink underline-offset-3 hover:underline"
        >
          {c.name}
        </Link>
        {c.description ? <span className="truncate text-[13px] text-muted">{c.description}</span> : null}
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
      <h2 id="how-runs" className="m-0 text-lg font-medium">
        How runs get connections
      </h2>
      <ul className="m-0 flex list-none flex-col p-0">
        {rows.map((r) => (
          <li
            key={r}
            className="border-t border-line py-3 text-sm leading-normal first:border-t-0 first:pt-0 last:pb-0"
          >
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
        <h2 id="recent-activity" className="m-0 text-lg font-medium">
          Recent activity
        </h2>
        <span className="text-[13px] text-muted">From the audit log</span>
      </div>
      {activity.isPending ? (
        <p className="m-0 text-sm text-muted">Loading…</p>
      ) : activity.isError ? (
        <p className="m-0 text-sm text-muted">Couldn’t load the audit log.</p>
      ) : entries.length === 0 ? (
        <p className="m-0 text-sm text-muted">Nothing yet.</p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {entries.map((e) => (
            <li
              key={e.id}
              className="flex items-start justify-between gap-3 border-t border-line py-3 first:border-t-0 first:pt-0 last:pb-0"
            >
              <span className="min-w-0 text-sm">
                <span className="block break-words">
                  <span translate="no" className="font-medium">
                    {activityName(e)}
                  </span>
                  : {activityText(e)}
                </span>
                <span className="block text-[13px] text-muted">{e.actor.name}</span>
              </span>
              <span className="shrink-0 text-[13px] text-muted">{activityWhen(e.at, now)}</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

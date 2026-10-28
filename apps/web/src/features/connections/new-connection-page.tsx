// Add a connection (canvas "Add a connection" in docs/design/screens/connections/connection-add.jpg):
// a two-pane picker. On the left, a searchable catalog of the platform's kinds and the organization's
// own ("Define a kind…" for owners and admins). On the right, the chosen kind's title and description,
// its fields (secret ones as write-only password inputs), a name and a description, the tools agents
// may use when the kind is an MCP server (not built yet: the controls are disabled), and Add <kind>.
// goald has no test of values before a connection exists (spec 4 §8.2), so Add opens the new
// connection, which offers its test. Secret values leave the page once, in the create request, and
// are cleared after it. A `next` path in the URL (the onboarding's Code step) takes the person back there instead.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useRouter, useSearch } from '@tanstack/react-router';
import { Info, Plus, Search } from 'lucide-react';
import { type FormEvent, type ReactNode, useDeferredValue, useId, useState } from 'react';
import { ApiError } from '../../api/client';
import { createConnection } from '../../api/connections';
import { connectionKindsQuery, connectionsQuery, keys } from '../../api/queries';
import type { Connection, ConnectionKind } from '../../api/types';
import { TopBarSlot } from '../../app/top-bar';
import { connectionName } from '../../shared/connections';
import { useOrg, useOrgHolds } from '../../shared/org';
import { reasons } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { cn } from '../../ui/cn';
import { Field, Input } from '../../ui/field';
import { NotAvailable } from '../../ui/not-available';
import { DefineKindDialog } from './define-kind-dialog';
import { Monogram, WriteOnly } from './parts';
import { SecretInput } from './secret-input';
import { kindSummary, refusalText } from './words';

export function NewConnectionPage() {
  const org = useOrg();
  const { kind: chosen, next } = useSearch({ from: '/o/$org/_org/connections/new' });
  const navigate = useNavigate({ from: '/o/$org/connections/new' });
  const manage = useOrgHolds('connection:manage');
  const kindsQuery = useQuery(connectionKindsQuery(org.id));
  const connections = useQuery(connectionsQuery(org.id)).data ?? [];
  const kinds = kindsQuery.data ?? [];
  const kind = kinds.find((k) => k.name === chosen);
  const [defining, setDefining] = useState(false);

  const choose = (name: string) => void navigate({ search: { kind: name }, replace: true });

  return (
    <>
      <TopBarSlot
        crumb="Add connection"
        actions={
          <Button asChild variant="white" className="h-11 md:h-8.5">
            <Link to="/o/$org/connections" params={{ org: org.slug }}>
              Cancel
            </Link>
          </Button>
        }
      />
      <h1 className="m-0 text-page-title">Add a connection</h1>

      {kindsQuery.isPending ? (
        <div aria-busy="true" className="rounded-card border border-line bg-card p-4">
          <span role="status" className="sr-only">
            Loading connection kinds
          </span>
        </div>
      ) : kindsQuery.isError ? (
        <div
          className="flex max-w-xl flex-col items-start gap-3 rounded-card border border-line bg-card p-4"
          role="alert"
        >
          <Callout tone="rework">Couldn’t load the connection kinds. Check your connection and try again.</Callout>
          <Button variant="white" onClick={() => void kindsQuery.refetch()}>
            Try again
          </Button>
        </div>
      ) : kinds.length === 0 ? (
        <p className="m-0 rounded-card border border-line bg-card p-4 text-body text-muted">
          {org.name} has no connection kinds yet.
        </p>
      ) : (
        <div className="grid overflow-hidden rounded-card border border-line bg-card @[960px]:grid-cols-[300px_minmax(0,1fr)]">
          <Catalog
            kinds={kinds}
            added={new Set(connections.map((c) => c.kind))}
            chosen={kind?.name}
            onChoose={choose}
            onDefine={manage ? () => setDefining(true) : undefined}
          />
          <section aria-labelledby="kind-details" className="flex min-w-0 flex-col gap-5 p-6">
            {kind ? (
              <>
                <div className="flex items-center gap-4">
                  <Monogram name={kind.title} className="size-11 text-heading" />
                  <div className="flex min-w-0 flex-col gap-1">
                    <h2 id="kind-details" className="m-0 text-detail-title">
                      {kind.title}
                    </h2>
                    <p className="m-0 text-secondary text-muted">{kind.description ?? kindSummary(kind)}</p>
                  </div>
                </div>
                {manage ? (
                  <ConnectionForm key={kind.name} kind={kind} connections={connections} next={next} />
                ) : (
                  <Callout tone="note">{reasons.connections(org.name)}</Callout>
                )}
              </>
            ) : (
              <p id="kind-details" className="m-0 text-body text-muted">
                Choose an app or an MCP server to see what it asks for.
              </p>
            )}
          </section>
        </div>
      )}

      {defining ? (
        <DefineKindDialog
          org={org.id}
          onClose={() => setDefining(false)}
          onDefined={(name) => {
            setDefining(false);
            choose(name);
          }}
        />
      ) : null}
    </>
  );
}

/** The catalog: a search, the platform's kinds, then the organization's own and "Define a kind…". */
function Catalog({
  kinds,
  added,
  chosen,
  onChoose,
  onDefine,
}: {
  kinds: ConnectionKind[];
  added: Set<string>;
  chosen: string | undefined;
  onChoose: (name: string) => void;
  onDefine: (() => void) | undefined;
}) {
  const [text, setText] = useState('');
  const q = useDeferredValue(text.trim().toLowerCase());
  const id = useId();
  const matches = (k: ConnectionKind) =>
    !q || [k.name, k.title, k.description ?? ''].some((s) => s.toLowerCase().includes(q));
  const byTitle = (a: ConnectionKind, b: ConnectionKind) => a.title.localeCompare(b.title);
  const platform = kinds.filter((k) => k.platform && matches(k)).sort(byTitle);
  const own = kinds.filter((k) => !k.platform && matches(k)).sort(byTitle);

  return (
    <nav
      aria-label="Catalog"
      className="flex min-w-0 flex-col gap-1 border-b border-line bg-panel p-3 @[960px]:border-r @[960px]:border-b-0"
    >
      <Field label={<span className="sr-only">Search the catalog</span>}>
        <span className="relative mb-2 flex">
          <Search
            size={16}
            strokeWidth={1.5}
            aria-hidden="true"
            className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-muted"
          />
          <Input
            type="search"
            value={text}
            placeholder="Search apps and MCP servers"
            onChange={(e) => setText(e.target.value)}
            className="pl-9"
            autoComplete="off"
          />
        </span>
      </Field>

      {platform.length > 0 ? (
        <CatalogGroup id={`${id}-catalog`} title="Catalog">
          {platform.map((k) => (
            <CatalogItem
              key={k.name}
              kind={k}
              added={added.has(k.name)}
              current={k.name === chosen}
              onChoose={onChoose}
            />
          ))}
        </CatalogGroup>
      ) : null}

      {own.length > 0 || onDefine ? (
        <CatalogGroup id={`${id}-own`} title="Your own">
          {own.map((k) => (
            <CatalogItem
              key={k.name}
              kind={k}
              added={added.has(k.name)}
              current={k.name === chosen}
              onChoose={onChoose}
            />
          ))}
          {onDefine ? (
            <button
              type="button"
              onClick={onDefine}
              className="flex min-h-11 items-center gap-3 rounded-control px-2 py-2 text-left text-body font-control text-ink hover:bg-hover"
            >
              <span
                aria-hidden="true"
                className="flex size-9 shrink-0 items-center justify-center rounded-control border border-dashed border-rule"
              >
                <Plus size={16} strokeWidth={1.75} />
              </span>
              Define a kind…
            </button>
          ) : null}
        </CatalogGroup>
      ) : null}

      {platform.length === 0 && own.length === 0 ? (
        <p className="m-0 px-2 py-2 text-secondary text-muted">No kind matches “{text}”.</p>
      ) : null}
    </nav>
  );
}

function CatalogGroup({ id, title, children }: { id: string; title: string; children: ReactNode }) {
  return (
    <section aria-labelledby={id} className="flex flex-col gap-1">
      <p id={id} className="m-0 px-2 pt-3 pb-1 text-meta font-control text-muted">
        {title}
      </p>
      {children}
    </section>
  );
}

function CatalogItem({
  kind,
  added,
  current,
  onChoose,
}: {
  kind: ConnectionKind;
  added: boolean;
  current: boolean;
  onChoose: (name: string) => void;
}) {
  return (
    <button
      type="button"
      aria-current={current ? 'true' : undefined}
      onClick={() => onChoose(kind.name)}
      className={cn(
        'flex min-h-11 min-w-0 items-center gap-3 rounded-control px-2 py-2 text-left text-ink hover:bg-hover',
        current && 'bg-card shadow-raised hover:bg-card',
      )}
    >
      <Monogram name={kind.title} />
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="font-control text-body">{kind.title}</span>
        <span className="truncate text-secondary text-muted">{kindSummary(kind)}</span>
      </span>
      {added ? <span className="shrink-0 text-meta font-control text-ok-ink">Added</span> : null}
    </button>
  );
}

/** The write-only fields' order and the name, then the tools (MCP kinds), the status line and Add. */
function ConnectionForm({
  kind,
  connections,
  next,
}: {
  kind: ConnectionKind;
  connections: Connection[];
  next: string | undefined;
}) {
  const org = useOrg();
  const client = useQueryClient();
  const navigate = useNavigate();
  const router = useRouter();
  const taken = connections.map((c) => c.name);
  const [name, setName] = useState(() => suggestedName(kind.name, taken));
  const [values, setValues] = useState<Record<string, string>>({});
  const [description, setDescription] = useState('');
  const [submitted, setSubmitted] = useState(false);

  const nameError = !connectionName.test(name)
    ? '3 to 40 lowercase letters, digits and dashes, starting with a letter.'
    : taken.includes(name)
      ? `${org.name} already has a connection named ${name}.`
      : undefined;
  const missing = (field: string) => {
    const f = kind.fields.find((x) => x.name === field);
    return f?.required && !f.default && !values[field]?.trim() ? `${f.title} is required.` : undefined;
  };

  const create = useMutation({
    // The secrets are in mutationFn's closure: forget the mutation once nothing shows it.
    gcTime: 0,
    mutationFn: () => {
      const fields: Record<string, string> = {};
      const secrets: Record<string, string> = {};
      for (const f of kind.fields) {
        const v = f.secret ? (values[f.name] ?? '') : (values[f.name] ?? '').trim();
        if (v !== '') (f.secret ? secrets : fields)[f.name] = v;
      }
      return createConnection(org.id, {
        name,
        kind: kind.name,
        ...(description.trim() ? { description: description.trim() } : {}),
        fields,
        secrets,
      });
    },
    onSuccess: async (created) => {
      // The secrets leave the page with the request.
      setValues({});
      client.setQueryData(keys.connection(org.id, created.name), created);
      await client.invalidateQueries({ queryKey: keys.connections(org.id) });
      if (next) {
        router.history.push(next);
        return;
      }
      await navigate({
        to: '/o/$org/connections/$connection',
        params: { org: org.slug, connection: created.name },
        search: { created: true },
      });
    },
  });
  const takenByGoald = create.error instanceof ApiError && create.error.code === 'connection-name-taken';
  // A taken name says so at once; a malformed one once Add was tried.
  const shownNameError = takenByGoald ? 'Taken' : submitted || taken.includes(name) ? nameError : undefined;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setSubmitted(true);
    if (nameError || kind.fields.some((f) => missing(f.name)) || create.isPending) return;
    create.mutate();
  };

  return (
    <form aria-label={`${kind.title} details`} onSubmit={submit} noValidate className="flex min-w-0 flex-col gap-5">
      <div className="grid grid-cols-1 gap-4 @[640px]:grid-cols-2">
        <Field label="Name" error={shownNameError} help={`How definitions name it. Lowercase, unique in ${org.name}.`}>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            maxLength={40}
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            translate="no"
            className="font-mono md:text-secondary"
          />
        </Field>
        {kind.fields
          .filter((f) => !f.secret)
          .map((f) => (
            <Field
              key={f.name}
              label={f.required ? f.title : `${f.title} (optional)`}
              error={submitted ? missing(f.name) : undefined}
              help={f.description ?? (f.default ? `Empty means ${f.default}.` : undefined)}
            >
              <Input
                value={values[f.name] ?? ''}
                placeholder={f.default}
                onChange={(e) => setValues((v) => ({ ...v, [f.name]: e.target.value }))}
                autoComplete="off"
                spellCheck={false}
              />
            </Field>
          ))}
        {kind.fields
          .filter((f) => f.secret)
          .map((f) => (
            <Field
              key={f.name}
              label={f.required ? f.title : `${f.title} (optional)`}
              aside={<WriteOnly />}
              error={submitted ? missing(f.name) : undefined}
              help={`${f.description ? `${f.description} ` : ''}Encrypted when you add the connection, never shown again. Later you can only replace it.`}
              className="col-span-full"
            >
              <SecretInput
                value={values[f.name] ?? ''}
                onChange={(e) => setValues((v) => ({ ...v, [f.name]: e.target.value }))}
              />
            </Field>
          ))}
        <Field label="Description (optional)" help="Shown on Connections and in the builder." className="col-span-full">
          <Input
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            maxLength={500}
            autoComplete="off"
          />
        </Field>
      </div>

      {kind.modes.includes('mcp') ? <ToolSettings /> : null}

      {create.error && !takenByGoald ? (
        <div role="alert">
          <Callout tone="rework">{refusalText(create.error)}</Callout>
        </div>
      ) : null}

      <p role="status" className="m-0 flex items-center gap-3 rounded-control bg-panel p-3 text-body text-ink-soft">
        <Info size={16} strokeWidth={1.75} aria-hidden="true" className="shrink-0" />
        <span>
          {kind.testable
            ? 'Once it exists, Test sends the kind’s check from goald with these values.'
            : 'This kind has no test: runs show whether it works.'}
        </span>
      </p>

      <div className="flex flex-wrap items-center justify-between gap-3 border-t border-line pt-5">
        <p className="m-0 text-secondary text-muted">Agents get it on their next run</p>
        <Button type="submit" variant="primary" className="h-11 md:h-8.5" disabled={create.isPending}>
          {create.isPending ? 'Adding…' : `Add ${kind.title}`}
        </Button>
      </div>
    </form>
  );
}

/**
 * Tools agents may use: one Allow / Ask first / Off setting for the whole server. Per-tool settings
 * and the "Ask first" setting are not built yet, so the three are disabled (ADR 0016, decision 4).
 */
function ToolSettings() {
  const settings = ['Allow', 'Ask first', 'Off'] as const;
  return (
    <fieldset className="m-0 flex min-w-0 flex-col gap-2 border-0 p-0">
      <legend className="mb-2 p-0 text-secondary font-control">Tools agents may use</legend>
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-control border border-line p-3">
        <span className="text-body">All of this server’s tools</span>
        <div className="inline-flex gap-1 rounded-control border border-line bg-panel p-1">
          {settings.map((setting) => (
            <NotAvailable key={setting}>
              <button
                type="button"
                aria-pressed={setting === 'Allow'}
                className={cn(
                  'h-8 rounded-chip px-3 text-secondary font-control text-ink-soft',
                  setting === 'Allow' && 'bg-card text-ink shadow-raised',
                )}
              >
                {setting}
              </button>
            </NotAvailable>
          ))}
        </div>
      </div>
    </fieldset>
  );
}

/** A name for the new connection: the kind's, or the kind's with a number when it is taken. */
function suggestedName(kind: string, taken: readonly string[]): string {
  if (!taken.includes(kind) && connectionName.test(kind)) return kind;
  for (let i = 2; ; i++) {
    const name = `${kind}-${i}`.slice(0, 40);
    if (!taken.includes(name)) return name;
  }
}

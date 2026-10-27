// New connection (spec 4 §10, canvas "Connection-New"): choose a kind (platform kinds and the
// organization's own, searchable; "Define a kind…" for owners and admins), then its fields
// (secret ones as write-only password inputs), a name and a description. Beside them, what a run
// gets from the kind and how runs use it. goald has no test of values before a connection exists
// (spec 4 §8.2), so Create opens the new connection, which offers its test. Secret values leave
// the page once, in the create request, and are cleared after it.
import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { Plus, Search } from 'lucide-react';
import { type FormEvent, useDeferredValue, useId, useState } from 'react';
import { ApiError } from '../../api/client';
import { createConnection } from '../../api/connections';
import { connectionKindsQuery, connectionsQuery, keys } from '../../api/queries';
import type { ConnectionKind } from '../../api/types';
import { connectionName, runEnv } from '../../shared/connections';
import { useOrg, useOrgHolds } from '../../shared/org';
import { reasons } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { Field, Input } from '../../ui/field';
import { descriptionClass, PageHeader } from '../../ui/page-header';
import { DefineKindDialog } from './define-kind-dialog';
import { EnvNames, Monogram, WriteOnly } from './parts';
import { SecretInput } from './secret-input';
import { kindSummary, modeSentence, refusalText } from './words';

export function NewConnectionPage() {
  const org = useOrg();
  const { kind: chosen } = useSearch({ from: '/o/$org/_org/connections/new' });
  const navigate = useNavigate({ from: '/o/$org/connections/new' });
  const manage = useOrgHolds('connection:manage');
  const { data: kinds } = useSuspenseQuery(connectionKindsQuery(org.id));
  const kind = kinds.find((k) => k.name === chosen);
  const [defining, setDefining] = useState(false);

  const choose = (name: string) => void navigate({ search: { kind: name }, replace: true });

  return (
    <>
      <nav aria-label="Breadcrumb" className="text-sm text-muted">
        <ol className="m-0 flex list-none gap-1.5 p-0">
          <li>
            <Link
              to="/o/$org/connections"
              params={{ org: org.slug }}
              className="text-muted underline underline-offset-3 hover:text-ink"
            >
              Connections
            </Link>
          </li>
          <li aria-hidden="true">/</li>
          <li aria-current="page">New connection</li>
        </ol>
      </nav>
      <PageHeader
        title="New connection"
        description={
          <p className={cn(descriptionClass, 'max-w-[720px]')}>
            Choose a kind, then fill in what it asks for. The kind decides the environment variables a run gets and
            whether agents use a CLI or an MCP server.
          </p>
        }
      />
      {manage ? null : <Callout tone="note">{reasons.connections(org.name)}</Callout>}

      <div className="grid items-start gap-4 @[960px]:grid-cols-[minmax(0,1fr)_minmax(0,380px)]">
        <div className="flex min-w-0 flex-col gap-4">
          <KindPicker
            kinds={kinds}
            orgName={org.name}
            chosen={kind?.name}
            onChoose={choose}
            onDefine={manage ? () => setDefining(true) : undefined}
          />
          {kind && manage ? <ConnectionForm key={kind.name} kind={kind} /> : null}
        </div>
        {kind ? <KindSide kind={kind} /> : null}
      </div>

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

function KindPicker({
  kinds,
  orgName,
  chosen,
  onChoose,
  onDefine,
}: {
  kinds: ConnectionKind[];
  orgName: string;
  chosen: string | undefined;
  onChoose: (name: string) => void;
  onDefine: (() => void) | undefined;
}) {
  const [text, setText] = useState('');
  const q = useDeferredValue(text.trim().toLowerCase());
  const id = useId();
  const shown = kinds.filter(
    (k) => !q || [k.name, k.title, k.description ?? ''].some((s) => s.toLowerCase().includes(q)),
  );
  const own = kinds.filter((k) => !k.platform).length;
  // The platform's kinds first, then the organization's own, each by title.
  const ordered = [...shown].sort(
    (a, b) => Number(a.platform === false) - Number(b.platform === false) || a.title.localeCompare(b.title),
  );

  return (
    <section
      aria-labelledby={`${id}-kind`}
      className="flex min-w-0 flex-col gap-3 rounded-card border border-line bg-card p-4 md:p-5"
    >
      <h2 id={`${id}-kind`} className="m-0 text-lg font-medium">
        Kind
      </h2>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Field label={<span className="sr-only">Find a kind</span>} className="w-full @[640px]:w-72">
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
              placeholder="Find a kind…"
              onChange={(e) => setText(e.target.value)}
              className="pl-9"
              autoComplete="off"
            />
          </span>
        </Field>
        <p className="m-0 text-[13px] text-muted">
          {kinds.length - own} platform kinds{own > 0 ? `, ${own} of ${orgName}’s` : ''}
        </p>
      </div>
      <fieldset
        aria-labelledby={`${id}-kind`}
        className="m-0 grid min-w-0 grid-cols-1 gap-2 border-0 p-0 @[520px]:grid-cols-2 @[760px]:grid-cols-3"
      >
        {ordered.map((k) => {
          const on = k.name === chosen;
          return (
            <label
              key={k.name}
              className={cn(
                'relative grid min-h-16 cursor-pointer grid-cols-[36px_minmax(0,1fr)] items-center gap-3 rounded-control border bg-card p-3 text-left text-ink transition-colors duration-200',
                'has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-run',
                on ? 'border-ink shadow-[0_0_0_1px_var(--color-ink)]' : 'border-line hover:border-rule',
              )}
            >
              <input
                type="radio"
                name={`${id}-kind`}
                value={k.name}
                checked={on}
                onChange={() => onChoose(k.name)}
                // Over the whole tile, unseen: the tile is the radio.
                className="absolute inset-0 m-0 size-full cursor-pointer opacity-0"
              />
              <Monogram name={k.title} />
              <span className="flex min-w-0 flex-col gap-0.5">
                <span className="text-[15px] font-medium">{k.title}</span>
                <span className="text-[13px] leading-snug text-muted">{kindSummary(k)}</span>
                {k.model || !k.platform ? (
                  <span className="mt-0.5 flex flex-wrap gap-1">
                    {k.model ? <Chip tone="you">Model</Chip> : null}
                    {k.platform ? null : <Chip tone="quiet">{orgName}</Chip>}
                  </span>
                ) : null}
              </span>
            </label>
          );
        })}
        {onDefine ? (
          <button
            type="button"
            onClick={onDefine}
            className="flex min-h-16 cursor-pointer items-center justify-center gap-2 rounded-control border border-dashed border-rule bg-transparent p-3 text-[15px] font-medium text-ink transition-colors duration-200 hover:bg-hover"
          >
            <Plus size={16} strokeWidth={1.75} aria-hidden="true" />
            Define a kind…
          </button>
        ) : null}
      </fieldset>
      {ordered.length === 0 ? <p className="m-0 text-sm text-muted">No kind matches “{text}”.</p> : null}
    </section>
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

function ConnectionForm({ kind }: { kind: ConnectionKind }) {
  const org = useOrg();
  const client = useQueryClient();
  const navigate = useNavigate();
  const { data: existing } = useSuspenseQuery(connectionsQuery(org.id));
  const taken = existing.map((c) => c.name);
  const [name, setName] = useState(() => suggestedName(kind.name, taken));
  const [values, setValues] = useState<Record<string, string>>({});
  const [description, setDescription] = useState('');
  const [submitted, setSubmitted] = useState(false);
  const id = useId();

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
      await navigate({
        to: '/o/$org/connections/$connection',
        params: { org: org.slug, connection: created.name },
        search: { created: true },
      });
    },
  });
  const takenByGoald = create.error instanceof ApiError && create.error.code === 'connection-name-taken';
  // A taken name says so at once; a malformed one once Create was tried.
  const shownNameError = takenByGoald ? 'Taken' : submitted || taken.includes(name) ? nameError : undefined;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setSubmitted(true);
    if (nameError || kind.fields.some((f) => missing(f.name)) || create.isPending) return;
    create.mutate();
  };

  return (
    <form
      aria-labelledby={`${id}-details`}
      onSubmit={submit}
      noValidate
      className="flex min-w-0 flex-col gap-4 rounded-card border border-line bg-card p-4 md:p-5"
    >
      <h2 id={`${id}-details`} className="m-0 text-lg font-medium">
        {kind.title} details
      </h2>
      <div className="grid grid-cols-1 gap-x-5 gap-y-4 @[640px]:grid-cols-2">
        <Field label="Name" error={shownNameError} help={`How definitions name it. Lowercase, unique in ${org.name}.`}>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            maxLength={40}
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            translate="no"
            className="font-mono md:text-sm"
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
              help={`${f.description ? `${f.description} ` : ''}Encrypted when you create the connection, never shown again. Later you can only replace it.`}
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
      {create.error && !takenByGoald ? (
        <div role="alert">
          <Callout tone="rework">{refusalText(create.error)}</Callout>
        </div>
      ) : null}
      <p className="m-0 text-[13px] leading-snug text-muted">
        {kind.testable
          ? 'Once it exists, Test sends the kind’s check from goald with these values.'
          : 'This kind has no test: runs show whether it works.'}
      </p>
      <div className="flex flex-wrap items-center justify-between gap-2 border-t border-line pt-4">
        <Button asChild variant="white" className="h-11 md:h-10">
          <Link to="/o/$org/connections" params={{ org: org.slug }}>
            Cancel
          </Link>
        </Button>
        <Button type="submit" variant="primary" className="h-11 md:h-10" disabled={create.isPending}>
          {create.isPending ? 'Creating…' : 'Create connection'}
        </Button>
      </div>
    </form>
  );
}

/** What a run gets from the kind and how runs use it (canvas "What a run gets" and "Modes"). */
function KindSide({ kind }: { kind: ConnectionKind }) {
  const env = runEnv(kind, { fields: {} });
  return (
    <div className="flex min-w-0 flex-col gap-4">
      <section
        aria-labelledby="run-gets"
        className="flex flex-col gap-3 rounded-card border border-line bg-card p-4 md:p-5"
      >
        <h2 id="run-gets" className="m-0 text-lg font-medium">
          What a run gets
        </h2>
        <p className="m-0 text-sm leading-normal text-muted">
          Set by the {kind.title} kind, for runs of factories that use the connection.
          {kind.mint ? ' Credentials are minted for each run; the stored keys never reach it.' : ''}
        </p>
        <EnvNames names={env} />
      </section>
      {kind.modes.length > 0 || kind.docs ? (
        <section
          aria-labelledby="modes"
          className="flex flex-col gap-3 rounded-card border border-line bg-card p-4 md:p-5"
        >
          <h2 id="modes" className="m-0 text-lg font-medium">
            Modes
          </h2>
          <dl className="m-0 flex flex-col">
            {kind.modes.map((m) => (
              <div
                key={m}
                className="grid grid-cols-[52px_minmax(0,1fr)] gap-3 border-t border-line py-2.5 first:border-t-0 first:pt-0"
              >
                <dt>
                  <Chip className="font-mono font-normal">{m === 'cli' ? 'CLI' : m === 'mcp' ? 'MCP' : m}</Chip>
                </dt>
                <dd className="m-0 text-sm leading-normal">{modeSentence(m, kind, env)}</dd>
              </div>
            ))}
          </dl>
          {kind.docs ? <p className="m-0 text-[13px] leading-normal text-muted">Agents read: “{kind.docs}”</p> : null}
        </section>
      ) : null}
    </div>
  );
}

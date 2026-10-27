// One connection (spec 4 §10, canvas "Connection"): its name, kind and status; Test and Save
// changes for owners and admins; the kind's note when credentials are minted per run; its fields
// (secret ones shown as set, with Replace secret…, never a value); what uses it; Delete, which
// needs the name typed and is refused while a factory's current version names it; and beside
// them what a run gets (the env names and modes, the kind's docs) and its recent use from the
// audit log. A rename is refused while it is used (the field says so). Members read it all.
import { useMutation, useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query';
import { Link, useNavigate, useParams, useSearch } from '@tanstack/react-router';
import { Check, Clock, RefreshCw } from 'lucide-react';
import { type FormEvent, useId, useState } from 'react';
import { toast } from 'sonner';
import { ApiError } from '../../api/client';
import { deleteConnection, updateConnection } from '../../api/connections';
import { connectionActivityQuery, connectionKindsQuery, connectionQuery, keys } from '../../api/queries';
import type { Connection, ConnectionKind, ConnectionKindField } from '../../api/types';
import { TopBarSlot } from '../../app/top-bar';
import { connectionName, modeLabels, namesText, statusLine, testedText } from '../../shared/connections';
import { useCreatedOnce } from '../../shared/created';
import { useOrg, useOrgHolds } from '../../shared/org';
import { reasons } from '../../shared/permissions';
import { useEdits } from '../../shared/use-edits';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { ConfirmDialog } from '../../ui/confirm-dialog';
import { Field, Input } from '../../ui/field';
import { descriptionClass, PageHeader } from '../../ui/page-header';
import { ReadOnly } from '../../ui/read-only';
import { EnvNames, ToneText, WriteOnly } from './parts';
import { ReplaceSecretDialog } from './replace-secret-dialog';
import { useTestConnection } from './use-test';
import { activityText, activityWhen, modeSentence, refusalText } from './words';

export function ConnectionPage() {
  const org = useOrg();
  const params = useParams({ from: '/o/$org/_org/connections/$connection' });
  const { created: justCreated } = useSearch({ from: '/o/$org/_org/connections/$connection' });
  const created = useCreatedOnce(justCreated);
  const { data: connection } = useSuspenseQuery(connectionQuery(org.id, params.connection));
  const { data: kinds } = useSuspenseQuery(connectionKindsQuery(org.id));
  const kind = kinds.find((k) => k.name === connection.kind);
  const manage = useOrgHolds('connection:manage');
  const auditor = useOrgHolds('audit:read');
  const tester = useTestConnection(org.id);
  const [replacing, setReplacing] = useState<ConnectionKindField | null>(null);
  const now = Date.now();
  const status = statusLine(connection, kind);
  const testable = manage && (kind?.testable ?? false);
  const testing = tester.isPending;

  const testButton = testable ? (
    <Button variant="white" className="h-11 md:h-10" disabled={testing} onClick={() => tester.mutate(connection.name)}>
      <RefreshCw size={16} strokeWidth={1.5} aria-hidden="true" className={cn(testing && 'motion-safe:animate-spin')} />
      {testing ? 'Testing…' : 'Test'}
    </Button>
  ) : null;

  return (
    <>
      <TopBarSlot crumb={connection.name} />
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
          <li aria-current="page" translate="no">
            {connection.name}
          </li>
        </ol>
      </nav>
      <PageHeader
        title={
          <span className="inline-flex flex-wrap items-center gap-3">
            <span translate="no">{connection.name}</span>
            <Chip tone="quiet" className="bg-panel font-normal tracking-normal">
              {kind?.title ?? connection.kind}
            </Chip>
          </span>
        }
        description={
          <p className={cn(descriptionClass, 'flex flex-wrap items-center gap-x-2 gap-y-1')}>
            {connection.description ? <span>{connection.description}.</span> : null}
            <ToneText
              tone={status.tone}
              text={
                connection.last_test && connection.status !== 'missing-secret'
                  ? `${status.text}, ${testedText(connection, now).toLowerCase()}`
                  : status.text
              }
              className="text-sm"
            />
          </p>
        }
        actions={<div className="flex flex-wrap gap-2">{testButton}</div>}
      />

      {created ? (
        <div role="status">
          <Callout tone="ok">
            {connection.name} is created.{' '}
            {kind?.testable
              ? 'Test it to check what you entered works.'
              : 'Factories can name it in their Connections now.'}
          </Callout>
        </div>
      ) : null}
      {connection.status === 'failing' && connection.last_test?.error ? (
        <Callout tone="rework">Its last test failed: {connection.last_test.error}.</Callout>
      ) : null}
      {connection.status === 'missing-secret' ? (
        <Callout tone="rework">A required secret isn’t set: runs that get this connection fail until it is.</Callout>
      ) : null}
      {kind?.mint ? (
        <section className="flex items-start gap-3 rounded-card border border-line bg-card px-4 py-3.5 text-[15px] leading-normal">
          <Clock size={18} strokeWidth={1.5} aria-hidden="true" className="mt-0.5 shrink-0 text-run" />
          <p className="m-0">
            <strong className="font-medium">Agents get short-lived credentials per run.</strong> When a run starts, Goal
            mints credentials from the stored keys that last the run’s timeout plus 15 minutes, and passes only those.
            The stored keys never reach a run; the IAM policy decides what they may do.
          </p>
        </section>
      ) : null}

      <div className="grid items-start gap-4 @[960px]:grid-cols-[minmax(0,1fr)_minmax(0,380px)]">
        <div className="flex min-w-0 flex-col gap-4">
          {manage ? null : <Callout tone="note">{reasons.connections(org.name)}</Callout>}
          {/* Edits survive a replaced secret and other changes; untouched fields follow the saved connection. */}
          <FieldsCard
            key={connection.id}
            connection={connection}
            kind={kind}
            manage={manage}
            onReplace={setReplacing}
          />
          <UsedByCard connection={connection} />
          {manage ? <DeleteCard connection={connection} /> : null}
        </div>
        <div className="flex min-w-0 flex-col gap-4">
          <RunGetsCard connection={connection} kind={kind} />
          {auditor ? <RecentUse org={org.id} connection={connection} now={now} /> : null}
        </div>
      </div>

      <ReplaceSecretDialog org={org.id} connection={connection} field={replacing} onClose={() => setReplacing(null)} />
    </>
  );
}

/** The editable part: name, the kind's fields and description; secrets shown as set, never a value. */
function FieldsCard({
  connection: c,
  kind,
  manage,
  onReplace,
}: {
  connection: Connection;
  kind: ConnectionKind | undefined;
  manage: boolean;
  onReplace: (field: ConnectionKindField) => void;
}) {
  const org = useOrg();
  const client = useQueryClient();
  const navigate = useNavigate();
  const id = useId();
  const edits = useEdits({ name: c.name, description: c.description, fields: c.fields });
  const { name, description, fields } = edits.value;
  const used = c.used_by.factories.length > 0;
  const plain = kind?.fields.filter((f) => !f.secret) ?? [];
  const secret = kind?.fields.filter((f) => f.secret) ?? [];
  const changedFields = plain.some((f) => (fields[f.name] ?? '') !== (c.fields[f.name] ?? ''));
  const dirty = name !== c.name || description !== c.description || changedFields;
  const nameError = connectionName.test(name)
    ? undefined
    : '3 to 40 lowercase letters, digits and dashes, starting with a letter.';

  const save = useMutation({
    mutationFn: () =>
      updateConnection(org.id, c.name, {
        ...(name !== c.name ? { name } : {}),
        ...(description !== c.description ? { description } : {}),
        ...(changedFields
          ? { fields: Object.fromEntries(Object.entries(fields).filter(([, v]) => v.trim() !== '')) }
          : {}),
      }),
    onSuccess: async (updated) => {
      // PATCH answers without what only GET has (env, docs): keep those from the page's copy.
      const before = client.getQueryData<Connection>(keys.connection(org.id, c.name));
      client.setQueryData<Connection>(keys.connection(org.id, updated.name), { ...before, ...updated });
      edits.reset();
      toast.success(`Saved ${updated.name}`, { description: 'Runs get it from their next start.' });
      if (updated.name !== c.name) {
        await navigate({
          to: '/o/$org/connections/$connection',
          params: { org: org.slug, connection: updated.name },
          replace: true,
        });
        // The old name is gone: refetching it would only answer 404.
        client.removeQueries({ queryKey: keys.connection(org.id, c.name), exact: true });
      }
      await client.invalidateQueries({ queryKey: keys.connections(org.id) });
    },
  });
  const refusal = save.error instanceof ApiError ? save.error : undefined;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!dirty || nameError || save.isPending) return;
    save.mutate();
  };

  const form = (
    <form
      aria-labelledby={`${id}-fields`}
      onSubmit={submit}
      noValidate
      className="flex min-w-0 flex-col gap-4 rounded-card border border-line bg-card p-4 md:p-5"
    >
      <h2 id={`${id}-fields`} className="m-0 text-lg font-medium">
        Fields
      </h2>
      <div className="grid grid-cols-1 gap-x-5 gap-y-4 @[640px]:grid-cols-2">
        <Field
          label="Name"
          error={nameError ?? (refusal?.code === 'connection-name-taken' ? 'Taken' : undefined)}
          help={
            used && manage
              ? `${namesText(c.used_by.factories.map((f) => f.name))}’s current version names it, so it can’t be renamed now.`
              : 'How definitions name it.'
          }
        >
          <Input
            value={name}
            onChange={(e) => edits.set('name', e.target.value)}
            readOnly={used}
            maxLength={40}
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            translate="no"
            className="font-mono md:text-sm"
          />
        </Field>
        {plain.map((f) => (
          <Field
            key={f.name}
            label={f.required ? f.title : `${f.title} (optional)`}
            help={f.description ?? (f.default ? `Empty means ${f.default}.` : undefined)}
          >
            <Input
              value={fields[f.name] ?? ''}
              placeholder={f.default}
              onChange={(e) => edits.set('fields', { ...fields, [f.name]: e.target.value })}
              autoComplete="off"
              spellCheck={false}
            />
          </Field>
        ))}
        {secret.map((f) => {
          const set = c.secrets_set.includes(f.name);
          return (
            <div key={f.name} className="col-span-full flex min-w-0 flex-col gap-1.5">
              <span className="flex flex-wrap items-center gap-2 text-sm font-medium">
                {f.title}
                <WriteOnly />
              </span>
              <div className="flex min-h-11 flex-wrap items-center justify-between gap-2 rounded-control border border-line bg-shell py-1.5 pr-1.5 pl-3">
                <span
                  className={cn('inline-flex items-center gap-2 text-[15px]', set ? 'text-ink' : 'text-rework-ink')}
                >
                  {set ? <Check size={16} strokeWidth={1.75} aria-hidden="true" className="text-ok" /> : null}
                  {set ? 'Set' : f.required ? 'Not set: runs that get it fail' : 'Not set'}
                </span>
                {manage ? (
                  <Button variant="white" className="h-10 md:h-9" onClick={() => onReplace(f)}>
                    {set ? 'Replace secret…' : 'Set secret…'}
                    <span className="sr-only">, {f.title}</span>
                  </Button>
                ) : null}
              </div>
              <p className="m-0 text-[13px] leading-snug text-muted">
                Never shown again. Replacing it asks for a fresh sign-in and applies from the next run.
              </p>
            </div>
          );
        })}
        <Field label="Description" className="col-span-full">
          <Input
            value={description}
            onChange={(e) => edits.set('description', e.target.value)}
            maxLength={500}
            autoComplete="off"
          />
        </Field>
      </div>
      {save.error && refusal?.code !== 'connection-name-taken' ? (
        <div role="alert">
          <Callout tone="rework">{refusalText(save.error)}</Callout>
        </div>
      ) : null}
      {manage ? (
        <div className="flex justify-end border-t border-line pt-4">
          <Button type="submit" variant="primary" className="h-11 md:h-10" disabled={!dirty || save.isPending}>
            {save.isPending ? 'Saving…' : 'Save changes'}
          </Button>
        </div>
      ) : null}
    </form>
  );
  return manage ? form : <ReadOnly>{form}</ReadOnly>;
}

function UsedByCard({ connection: c }: { connection: Connection }) {
  const org = useOrg();
  const id = useId();
  return (
    <section
      aria-labelledby={`${id}-used`}
      className="flex min-w-0 flex-col gap-3 rounded-card border border-line bg-card p-4 md:p-5"
    >
      <h2 id={`${id}-used`} className="m-0 text-lg font-medium">
        Used by
      </h2>
      {c.used_by.factories.length === 0 ? (
        <p className="m-0 text-sm text-muted">No factory’s current version names it.</p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {c.used_by.factories.map((f) => (
            <li key={f.id} className="border-t border-line py-2.5 text-[15px] first:border-t-0 first:pt-0">
              <Link
                to="/o/$org/f/$factory/definition/$section"
                params={{ org: org.slug, factory: f.name, section: 'connections' }}
                className="text-ink underline underline-offset-3 hover:text-run"
                translate="no"
              >
                {f.name}
              </Link>
              <span className="text-muted">, its current version</span>
            </li>
          ))}
        </ul>
      )}
      <p className="m-0 text-sm text-muted">
        {c.used_by.knowledge_sources.length > 0
          ? `Knowledge sources synced through it: ${namesText(c.used_by.knowledge_sources)}.`
          : 'No knowledge source uses it.'}
      </p>
    </section>
  );
}

function DeleteCard({ connection: c }: { connection: Connection }) {
  const org = useOrg();
  const client = useQueryClient();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const reasonId = useId();
  const users = [...c.used_by.factories.map((f) => f.name), ...c.used_by.knowledge_sources];
  const remove = useMutation({
    mutationFn: () => deleteConnection(org.id, c.name),
    onSuccess: async () => {
      setOpen(false);
      toast.success(`Deleted ${c.name}`);
      client.removeQueries({ queryKey: keys.connection(org.id, c.name) });
      await client.invalidateQueries({ queryKey: keys.connections(org.id) });
      await navigate({ to: '/o/$org/connections', params: { org: org.slug } });
    },
  });

  return (
    <section className="flex flex-wrap items-center justify-between gap-3 rounded-card border border-line bg-card p-4 md:p-5">
      <div className="flex min-w-0 max-w-[560px] flex-col gap-1">
        <h2 className="m-0 text-[15px] font-medium">
          Delete <span translate="no">{c.name}</span>
        </h2>
        <p id={reasonId} className="m-0 text-sm leading-normal text-muted">
          {users.length > 0
            ? `${namesText(users)} use${users.length === 1 ? 's' : ''} it, so Goal refuses until ${users.length === 1 ? 'its definition no longer names' : 'their definitions no longer name'} it. Then you type the name to confirm.`
            : 'Runs that start after it is gone can’t get it. You type the name to confirm.'}
        </p>
      </div>
      <Button
        variant="rework"
        className="h-11 md:h-10"
        aria-disabled={users.length > 0 || undefined}
        aria-describedby={reasonId}
        onClick={() => {
          if (users.length === 0) setOpen(true);
        }}
      >
        Delete…
      </Button>
      <ConfirmDialog
        open={open}
        onOpenChange={(next) => {
          setOpen(next);
          if (!next) remove.reset();
        }}
        title={`Delete ${c.name}?`}
        description="Its secrets are destroyed. This can’t be undone."
        confirmLabel="Delete connection"
        pendingLabel="Deleting…"
        tone="danger"
        confirmText={c.name}
        pending={remove.isPending}
        onConfirm={() => remove.mutate()}
      >
        {remove.error ? (
          <p role="alert" className="m-0 text-sm text-rework-ink">
            {refusalText(remove.error)}
          </p>
        ) : null}
      </ConfirmDialog>
    </section>
  );
}

function RunGetsCard({ connection: c, kind }: { connection: Connection; kind: ConnectionKind | undefined }) {
  const id = useId();
  const env = c.env ?? [];
  const modes = modeLabels(kind);
  return (
    <section
      aria-labelledby={`${id}-gets`}
      className="flex min-w-0 flex-col gap-3 rounded-card border border-line bg-card p-4 md:p-5"
    >
      <h2 id={`${id}-gets`} className="m-0 text-lg font-medium">
        What a run gets
      </h2>
      <p className="m-0 text-sm leading-normal text-muted">
        Environment variables, set when the box of a run that uses it starts.
        {kind?.mint ? ' Their values are minted for the run.' : ''}
      </p>
      <EnvNames names={env} />
      {modes.length > 0 && kind ? (
        <dl className="m-0 flex flex-col">
          {kind.modes.map((m, i) => (
            <div key={m} className="grid grid-cols-[52px_minmax(0,1fr)] gap-3 border-t border-line py-2.5">
              <dt>
                <Chip className="font-mono font-normal">{modes[i]}</Chip>
              </dt>
              <dd className="m-0 text-sm leading-normal">{modeSentence(m, kind, env)}</dd>
            </div>
          ))}
        </dl>
      ) : null}
      {c.docs ? <p className="m-0 text-[13px] leading-normal text-muted">The kind tells agents: “{c.docs}”</p> : null}
    </section>
  );
}

function RecentUse({ org, connection, now }: { org: string; connection: Connection; now: number }) {
  const id = useId();
  const activity = useQuery(connectionActivityQuery(org, connection.id));
  const entries = activity.data?.entries ?? [];
  const sentence = (text: string) => text.charAt(0).toUpperCase() + text.slice(1);
  return (
    <section
      aria-labelledby={`${id}-use`}
      className="flex min-w-0 flex-col gap-3 rounded-card border border-line bg-card p-4 md:p-5"
    >
      <div className="flex items-baseline justify-between gap-3">
        <h2 id={`${id}-use`} className="m-0 text-lg font-medium">
          Recent use
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
              className="flex items-start justify-between gap-3 border-t border-line py-2.5 first:border-t-0 first:pt-0"
            >
              <span className="min-w-0 text-sm">
                <span className="block break-words">{sentence(activityText(e))}</span>
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

// One knowledge source (spec 4 §8.2, §10): its kind, what it reads and its live status; Sync now;
// for an upload source the drop zone; its documents with their failures, a page at a time, each
// one's indexed text over the page (?document=); and beside them its settings (title, schedule,
// what a synced source reads), the factories that may search it and Delete, which needs the name
// typed and is refused while a factory's current version lists it. Members read it all.
import { useMutation, useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query';
import { Link, useNavigate, useParams, useSearch } from '@tanstack/react-router';
import { type FormEvent, useId, useState } from 'react';
import { toast } from 'sonner';
import { deleteKnowledgeSource, updateKnowledgeSource } from '../../api/knowledge';
import { connectionKindsQuery, connectionsQuery, keys, knowledgeSourceQuery } from '../../api/queries';
import type { KnowledgeSource } from '../../api/types';
import { namesText } from '../../shared/connections';
import { plural } from '../../shared/format';
import { sourceKinds, sourceSummary } from '../../shared/knowledge';
import { useOrg, useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { ConfirmDialog } from '../../ui/confirm-dialog';
import { Field, Input, NativeSelect } from '../../ui/field';
import { descriptionClass, PageHeader } from '../../ui/page-header';
import { ReadOnly } from '../../ui/read-only';
import { type Config, ConfigFields, configErrors, readerOf, trimmed } from './config-fields';
import { DocumentDialog, DocumentList } from './documents';
import { StatusText, SyncNowButton, useSyncSource } from './parts';
import { DropZone, UploadRows, useUploader } from './upload';
import { everyLabel, refusalText, sourceStatus, syncChoices, syncTimes } from './words';

const card = 'flex min-w-0 flex-col gap-3 rounded-card border border-line bg-card p-4 md:p-5';

export function SourcePage() {
  const org = useOrg();
  const params = useParams({ from: '/o/$org/_org/knowledge/$source' });
  const { created, document } = useSearch({ from: '/o/$org/_org/knowledge/$source' });
  const { data: source } = useSuspenseQuery(knowledgeSourceQuery(org.id, params.source));
  const manage = useOrgHolds('knowledge:manage');
  const sync = useSyncSource(org.id);
  const status = sourceStatus(source);
  const times = syncTimes(source, Date.now());
  const docsId = useId();

  return (
    <>
      <nav aria-label="Breadcrumb" className="text-sm text-muted">
        <ol className="m-0 flex list-none gap-1.5 p-0">
          <li>
            <Link
              to="/o/$org/knowledge"
              params={{ org: org.slug }}
              className="text-muted underline underline-offset-3 hover:text-ink"
            >
              Knowledge
            </Link>
          </li>
          <li aria-hidden="true">/</li>
          <li aria-current="page">{source.title}</li>
        </ol>
      </nav>
      <PageHeader
        title={
          <span className="inline-flex flex-wrap items-center gap-3">
            <span>{source.title}</span>
            <Chip tone="quiet" className="bg-control font-normal tracking-normal">
              {sourceKinds[source.kind].label}
            </Chip>
          </span>
        }
        description={
          <div className={cn(descriptionClass, 'flex flex-col gap-1')}>
            <p className="m-0 break-words" translate="no">
              <span className="font-mono text-sm text-ink">{source.name}</span>, {sourceSummary(source)}
            </p>
            <p className="m-0 flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
              <StatusText line={{ tone: status.tone, text: status.text }} busy={source.status === 'syncing'} />
              {source.kind === 'live' ? null : (
                <span>
                  {source.status === 'syncing'
                    ? 'Syncing now'
                    : source.last_synced_at
                      ? `${source.kind === 'upload' ? 'Last indexed' : 'Last sync'} ${times.last}`
                      : 'Not synced yet'}
                  {times.next && source.kind === 'synced' ? `. ${times.next}` : ''}.
                </span>
              )}
            </p>
          </div>
        }
        actions={
          manage && source.kind === 'synced' ? (
            <SyncNowButton source={source} sync={sync} className="h-11 md:h-10" />
          ) : null
        }
      />

      {created ? (
        <div role="status">
          <Callout tone="ok">{createdText(source)}</Callout>
        </div>
      ) : null}
      {status.tone === 'rework' ? <Callout tone="rework">Its last sync failed. {status.detail}</Callout> : null}

      <div className="grid items-start gap-4 @[960px]:grid-cols-[minmax(0,1fr)_minmax(0,380px)]">
        <div className="flex min-w-0 flex-col gap-4">
          {source.kind === 'upload' && manage ? <UploadCard source={source} /> : null}
          {source.kind === 'live' ? (
            <section className={card}>
              <h2 className="m-0 text-lg font-medium">Documents</h2>
              <p className="m-0 text-sm leading-normal text-muted">
                A live source stores nothing. Factories that list it get the tools of{' '}
                <span translate="no">{source.connection}</span>, which search at question time.
              </p>
            </section>
          ) : (
            <section aria-labelledby={docsId} className={card}>
              <div className="flex items-baseline justify-between gap-3">
                <h2 id={docsId} className="m-0 text-lg font-medium">
                  Documents
                </h2>
                <span className="text-[13px] text-muted">{plural(source.documents, 'document')}</span>
              </div>
              <DocumentList source={source} manage={manage} />
            </section>
          )}
        </div>
        <div className="flex min-w-0 flex-col gap-4">
          {/* A change saved anywhere (here, another tab, the CLI) starts the form over from it. */}
          <SettingsCard key={`${source.id}:${source.updated_at}`} source={source} manage={manage} />
          <UsedByCard source={source} />
          {manage ? <DeleteCard source={source} /> : null}
        </div>
      </div>

      <DocumentDialog source={source} id={document} />
    </>
  );
}

function createdText(s: KnowledgeSource): string {
  switch (s.kind) {
    case 'upload':
      return `${s.title} is added. Upload files to give agents something to search.`;
    case 'synced':
      return `${s.title} is added. Its first sync starts now; this page updates when it ends.`;
    default:
      return `${s.title} is added. Factories that list it get ${s.connection}’s tools.`;
  }
}

function UploadCard({ source }: { source: KnowledgeSource }) {
  const org = useOrg();
  const uploader = useUploader(org.id);
  const id = useId();
  const over = uploader.rows.some((r) => r.state === 'uploaded' || r.state === 'refused');
  return (
    <section aria-labelledby={id} className={card}>
      <div className="flex items-baseline justify-between gap-3">
        <h2 id={id} className="m-0 text-lg font-medium">
          Upload
        </h2>
        {over && !uploader.busy ? (
          <Button variant="ghost" className="h-11 md:h-8" onClick={uploader.clear}>
            Clear the list
          </Button>
        ) : null}
      </div>
      <DropZone onFiles={(files) => void uploader.upload(source.name, uploader.add(files))} />
      <UploadRows rows={uploader.rows} />
    </section>
  );
}

/** Title, schedule and what a synced source reads; the name, kind and connection never change. */
function SettingsCard({ source: s, manage }: { source: KnowledgeSource; manage: boolean }) {
  const org = useOrg();
  const client = useQueryClient();
  const id = useId();
  // The reader is the connection kind's sync; without connection:read, what the config's keys say.
  const read = useOrgHolds('connection:read');
  const connections = useQuery({ ...connectionsQuery(org.id), enabled: read }).data ?? [];
  const kinds = useQuery({ ...connectionKindsQuery(org.id), enabled: read }).data ?? [];
  const kind = kinds.find((k) => k.name === connections.find((c) => c.name === s.connection)?.kind);
  const reader = s.kind === 'synced' ? readerOf(kind?.sync, s.config) : undefined;
  const [title, setTitle] = useState(s.title);
  const [every, setEvery] = useState(s.sync_every_seconds);
  const [config, setConfig] = useState<Config>(s.config);
  const [submitted, setSubmitted] = useState(false);
  const changedConfig = JSON.stringify(trimmed(config)) !== JSON.stringify(trimmed(s.config));
  const dirty = title.trim() !== s.title || every !== s.sync_every_seconds || changedConfig;
  const titleError = title.trim() === '' ? 'A title is required.' : undefined;
  const errors = configErrors(reader, config);

  const save = useMutation({
    mutationFn: () =>
      updateKnowledgeSource(org.id, s.name, {
        ...(title.trim() !== s.title ? { title: title.trim() } : {}),
        ...(every !== s.sync_every_seconds ? { sync_every_seconds: every } : {}),
        ...(changedConfig ? { config: trimmed(config) } : {}),
      }),
    onSuccess: async (updated) => {
      client.setQueryData(keys.knowledgeSource(org.id, updated.name), updated);
      toast.success(`Saved ${updated.title}`, {
        description: changedConfig ? 'The next sync reads what you changed.' : undefined,
      });
      await client.invalidateQueries({ queryKey: keys.knowledge(org.id) });
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setSubmitted(true);
    if (!dirty || titleError || Object.keys(errors).length > 0 || save.isPending) return;
    save.mutate();
  };

  const choices = syncChoices.some((c) => c.seconds === s.sync_every_seconds)
    ? syncChoices
    : [...syncChoices, { seconds: s.sync_every_seconds, label: everyLabel(s.sync_every_seconds) }];

  const form = (
    <form aria-labelledby={id} onSubmit={submit} noValidate className={card}>
      <h2 id={id} className="m-0 text-lg font-medium">
        Settings
      </h2>
      <Field
        label="Title"
        error={submitted ? titleError : undefined}
        help="What people see on Knowledge and in the builder."
      >
        <Input value={title} onChange={(e) => setTitle(e.target.value)} maxLength={120} autoComplete="off" />
      </Field>
      <Field label="Name" help="How definitions list it. It never changes.">
        <Input value={s.name} readOnly translate="no" className="font-mono md:text-sm" />
      </Field>
      {s.kind === 'synced' ? (
        <>
          <Field label="Connection" help="It never changes: add another source to read another connection.">
            <Input value={s.connection ?? ''} readOnly translate="no" className="font-mono md:text-sm" />
          </Field>
          <ConfigFields
            reader={reader}
            connection={s.connection ?? ''}
            config={config}
            errors={submitted ? errors : {}}
            onChange={setConfig}
          />
          <Field label="Sync every" help="At least 15 minutes. Only changed documents are fetched again.">
            <NativeSelect value={every} onChange={(e) => setEvery(Number(e.target.value))}>
              {choices.map((c) => (
                <option key={c.seconds} value={c.seconds}>
                  {c.label}
                </option>
              ))}
            </NativeSelect>
          </Field>
        </>
      ) : null}
      {save.error ? (
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

function UsedByCard({ source: s }: { source: KnowledgeSource }) {
  const org = useOrg();
  const id = useId();
  return (
    <section aria-labelledby={id} className={card}>
      <h2 id={id} className="m-0 text-lg font-medium">
        Used by
      </h2>
      {s.used_by.factories.length === 0 ? (
        <p className="m-0 text-sm leading-normal text-muted">
          No factory’s current version lists it yet. A factory’s Definition says which sources its agents may search,
          under Connections and knowledge.
        </p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {s.used_by.factories.map((f) => (
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
      {s.connection ? (
        <p className="m-0 text-sm text-muted">
          Reads the connection{' '}
          <Link
            to="/o/$org/connections/$connection"
            params={{ org: org.slug, connection: s.connection }}
            className="font-mono text-ink underline underline-offset-3 hover:text-run"
            translate="no"
          >
            {s.connection}
          </Link>
          .
        </p>
      ) : null}
    </section>
  );
}

function DeleteCard({ source: s }: { source: KnowledgeSource }) {
  const org = useOrg();
  const client = useQueryClient();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const reasonId = useId();
  const users = s.used_by.factories.map((f) => f.name);
  const remove = useMutation({
    mutationFn: () => deleteKnowledgeSource(org.id, s.name),
    onSuccess: async () => {
      setOpen(false);
      toast.success(`Deleted ${s.title}`);
      client.removeQueries({ queryKey: keys.knowledgeSource(org.id, s.name) });
      await client.invalidateQueries({ queryKey: keys.knowledge(org.id) });
      await navigate({ to: '/o/$org/knowledge', params: { org: org.slug } });
    },
  });

  return (
    <section className="flex flex-wrap items-center justify-between gap-3 rounded-card border border-line bg-card p-4 md:p-5">
      <div className="flex min-w-0 max-w-[560px] flex-col gap-1">
        <h2 className="m-0 text-[15px] font-medium">Delete {s.title}</h2>
        <p id={reasonId} className="m-0 text-sm leading-normal text-muted">
          {users.length > 0
            ? `${namesText(users)} may search it, so Goal refuses until ${users.length === 1 ? 'its definition no longer lists' : 'their definitions no longer list'} it. Then you type the name to confirm.`
            : `Its documents and their index go with it${s.kind === 'upload' ? ', and the uploaded files' : ''}. You type the name to confirm.`}
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
        title={`Delete ${s.title}?`}
        description={`${plural(s.documents, 'document')} and ${s.documents === 1 ? 'its' : 'their'} index go with it. This can’t be undone.`}
        confirmLabel="Delete source"
        pendingLabel="Deleting…"
        tone="danger"
        confirmText={s.name}
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

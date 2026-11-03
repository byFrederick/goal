// A knowledge source (ADR 0038 decision 12, canvas "Knowledge-Source" and "Mobile-Knowledge-Source"):
// its files with the reads of the last 7 days, the latest sync, its settings, the line agents see in
// their prompt, and the factories and agents that read it, with Add to a factory (which writes that
// factory's draft and never publishes). Managers sync, upload, edit and delete from it. ?find= filters
// the files by path or title; ?created says it was just added here.
import { useMutation, useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query';
import { useNavigate, useParams, useSearch } from '@tanstack/react-router';
import { Plus } from 'lucide-react';
import { type FormEvent, useId, useState } from 'react';
import { toast } from 'sonner';
import { deleteKnowledgeSource, updateKnowledgeSource } from '../../api/knowledge';
import {
  factoriesQuery,
  keys,
  knowledgeFilesQuery,
  knowledgeReadsQuery,
  knowledgeSourceQuery,
} from '../../api/queries';
import type { KnowledgeSource, KnowledgeSourceReader } from '../../api/types';
import { TopBarSlot } from '../../app/top-bar';
import { namesText } from '../../shared/connections';
import { useCreatedOnce } from '../../shared/created';
import { formatClock, plural } from '../../shared/format';
import { useOrg, useOrgHolds } from '../../shared/org';
import { useEdits } from '../../shared/use-edits';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { ConfirmDialog } from '../../ui/confirm-dialog';
import { Field, Input, NativeSelect, Textarea } from '../../ui/field';
import { ReadOnly } from '../../ui/read-only';
import { SearchField } from '../../ui/search-field';
import { Skeleton, SkeletonRows } from '../../ui/skeleton';
import { ConfigFields, configErrors, readerOf, trimmed } from './config-fields';
import { listInDrafts } from './draft';
import { FactoryCheckboxes } from './factory-checkboxes';
import { agentLine, readsByPath, sizeOf } from './facts';
import { KindLabel, StatusText, SyncNowButton, useSyncSource } from './parts';
import { SecretField } from './secret-field';
import { DropZone, UploadRows, useUploader } from './upload';
import { everyLabel, formatBytes, refusalText, sourceStatus, syncChoices } from './words';

const card = 'flex min-w-0 flex-col gap-3 rounded-card border border-line bg-card p-4 md:p-5';
const panel = 'flex min-w-0 flex-col rounded-card border border-line bg-card';

export function SourcePage() {
  const org = useOrg();
  const params = useParams({ from: '/o/$org/_org/knowledge/$source' });
  const { created: justCreated } = useSearch({ from: '/o/$org/_org/knowledge/$source' });
  const created = useCreatedOnce(justCreated);
  const { data: source } = useSuspenseQuery(knowledgeSourceQuery(org.id, params.source));
  const manage = useOrgHolds('knowledge:manage');
  const sync = useSyncSource(org.id);
  const status = sourceStatus(source);
  const syncs = source.kind !== 'upload' && source.kind !== 'live';
  const at = source.last_synced_at && source.status !== 'syncing' ? formatClock(source.last_synced_at) : undefined;

  return (
    <>
      <TopBarSlot
        crumb={source.name}
        actions={manage && syncs ? <SyncNowButton source={source} sync={sync} className="h-11 md:h-8.5" /> : null}
      />
      <header className="flex min-w-0 flex-col gap-1">
        <h1 className="m-0 flex flex-wrap items-center gap-3 text-page-title">
          <span translate="no" className="font-mono break-all">
            {source.name}
          </span>
          <Chip>
            <KindLabel kind={source.kind} />
          </Chip>
          <StatusText
            line={{ tone: status.tone, text: status.text }}
            time={at}
            busy={source.status === 'syncing'}
            className="text-body font-normal"
          />
        </h1>
        {source.read_when ? <p className="m-0 text-body break-words text-muted">{source.read_when}</p> : null}
      </header>

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
              <h2 className="m-0 text-heading">Files</h2>
              <p className="m-0 text-body leading-normal text-muted">
                A live source stores no files. Factories that list it get the tools of{' '}
                <span translate="no">{source.connection}</span>, which search at question time.
              </p>
            </section>
          ) : (
            <>
              <FilesCard source={source} />
              <LatestSyncCard source={source} />
            </>
          )}
        </div>
        <div className="flex min-w-0 flex-col gap-4">
          {/* Edits survive a sync's end and other changes; untouched fields follow the saved source. */}
          <SettingsCard key={source.id} source={source} manage={manage} />
          <AgentLineCard source={source} />
          <ReadByCard source={source} manage={manage} />
          {manage ? <DeleteCard source={source} /> : null}
        </div>
      </div>
    </>
  );
}

function createdText(s: KnowledgeSource): string {
  switch (s.kind) {
    case 'upload':
      return `${s.title} is added. Upload files to give agents something to read.`;
    case 'live':
      return `${s.title} is added. Factories that list it get the tools of ${s.connection}.`;
    default:
      return `${s.title} is added. Its first sync starts now; this page updates when it ends.`;
  }
}

/** The files of the current snapshot with their reads of the last 7 days; ?find= filters them. */
function FilesCard({ source }: { source: KnowledgeSource }) {
  const org = useOrg();
  const search = useSearch({ from: '/o/$org/_org/knowledge/$source' });
  const navigate = useNavigate({ from: '/o/$org/knowledge/$source' });
  const find = search.find ?? '';
  const files = useQuery(knowledgeFilesQuery(org.id, source.name, find.trim()));
  const reads = readsByPath(useQuery(knowledgeReadsQuery(org.id, source.name)).data ?? []);
  const size = sizeOf(source);
  const rows = files.data ?? [];

  return (
    <section aria-labelledby="files-h" className={panel}>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2 px-5 py-3">
        <h2 id="files-h" className="m-0 text-heading">
          Files
        </h2>
        {size ? (
          <span className="text-secondary text-muted">
            {size.files} · {size.bytes}
          </span>
        ) : null}
        <SearchField
          value={find}
          onChange={(v) =>
            void navigate({ search: (prev) => ({ ...prev, find: v === '' ? undefined : v }), replace: true })
          }
          label="Filter by path"
          className="ml-auto max-w-xs"
        />
      </div>
      <Skeleton
        ready={!files.isPending}
        placeholder={
          <div className="border-t border-line p-4">
            <SkeletonRows rows={6} />
          </div>
        }
      >
        {files.isError ? (
          <div role="alert" className="border-t border-line p-4">
            <Callout tone="rework">Couldn’t load the files. Try again.</Callout>
          </div>
        ) : rows.length === 0 ? (
          <p className="m-0 border-t border-line px-5 py-4 text-body text-muted">
            {find
              ? `No file matches “${find}”.`
              : source.kind === 'upload'
                ? 'No files yet. Drop some above.'
                : 'No files yet. The first sync writes them.'}
          </p>
        ) : (
          <table className="w-full border-collapse text-body">
            <caption className="sr-only">Files of {source.name}</caption>
            <thead>
              <tr className="bg-panel text-left text-secondary text-muted [&>th]:border-t [&>th]:border-line [&>th]:px-5 [&>th]:py-2 [&>th]:font-control">
                <th scope="col">Path</th>
                <th scope="col" className="text-right">
                  Size
                </th>
                <th scope="col" className="text-right">
                  Reads, 7 days
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((f) => (
                <tr
                  key={f.path}
                  className="animate-row-in align-top [&>td]:border-t [&>td]:border-line [&>td]:px-5 [&>td]:py-3"
                >
                  <td className="min-w-0">
                    <span translate="no" className="font-mono break-all">
                      {f.path}
                    </span>
                    {f.title ? <span className="block text-secondary break-words text-muted">{f.title}</span> : null}
                  </td>
                  <td className="text-right whitespace-nowrap tabular-nums">{formatBytes(f.bytes)}</td>
                  <td className="text-right tabular-nums">{reads.get(f.path) ?? 0}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Skeleton>
    </section>
  );
}

/** The newest snapshot: when it was written, its files and what failed. */
function LatestSyncCard({ source }: { source: KnowledgeSource }) {
  const n = source.latest_snapshot;
  return (
    <section aria-labelledby="latest-h" className={panel}>
      <div className="flex items-baseline justify-between gap-3 px-5 py-3">
        <h2 id="latest-h" className="m-0 text-heading">
          Latest sync
        </h2>
        {n ? <span className="text-secondary text-muted">{formatClock(n.synced_at)}</span> : null}
      </div>
      <p className="m-0 border-t border-line px-5 py-4 text-body break-words">
        {n
          ? `${plural(n.files, 'file')}, ${formatBytes(n.bytes)}.${n.failed > 0 ? ` ${plural(n.failed, 'file')} failed and kept their last good version.` : ''} Runs that started earlier keep the files they pinned.`
          : 'Nothing has synced yet.'}
        {source.status === 'failed' && source.last_error ? ` The last try failed: ${source.last_error}.` : ''}
      </p>
    </section>
  );
}

/** The line agents see for the source in every run's prompt (the Knowledge section). */
function AgentLineCard({ source }: { source: KnowledgeSource }) {
  const org = useOrg();
  const files = useQuery(knowledgeFilesQuery(org.id, source.name)).data ?? [];
  return (
    <section aria-labelledby="line-h" className={panel}>
      <div className="flex items-baseline justify-between gap-3 px-5 py-3">
        <h2 id="line-h" className="m-0 text-heading">
          What agents see
        </h2>
        <span className="text-secondary text-muted">In every run’s prompt</span>
      </div>
      <p className="m-0 mx-4 mb-4 rounded-control bg-panel p-3 font-mono text-secondary break-words">
        {agentLine(source, files)}
      </p>
    </section>
  );
}

/** Who reads the source in a factory: every agent, or the agents, stations, lanes and work types its entry names. */
function readerScope(r: KnowledgeSourceReader): string {
  const parts = [
    r.agents.length > 0 ? namesText(r.agents) : '',
    r.stations.length > 0 ? `at ${namesText(r.stations)}` : '',
    r.lanes.length > 0 ? `in ${namesText(r.lanes)}` : '',
    r.work_types.length > 0 ? `on ${namesText(r.work_types)}` : '',
  ].filter(Boolean);
  return parts.length > 0 ? parts.join(', ') : 'Every agent';
}

function ReadByCard({ source, manage }: { source: KnowledgeSource; manage: boolean }) {
  const org = useOrg();
  const client = useQueryClient();
  const readers = source.used_by.readers;
  const all = (useQuery(factoriesQuery(org.id)).data ?? []).filter((f) => !f.archived_at);
  const rest = all.filter((f) => !readers.some((r) => r.id === f.id));
  const [adding, setAdding] = useState(false);
  const [picked, setPicked] = useState<string[]>([]);

  const add = useMutation({
    mutationFn: () =>
      listInDrafts(
        org.id,
        rest.filter((f) => picked.includes(f.id)),
        source.name,
      ),
    onSuccess: async (results) => {
      const failed = results.filter((r) => r.outcome === 'failed');
      if (failed.length > 0) {
        toast.error(`Couldn’t list ${source.name} in ${failed.map((r) => r.factory).join(', ')}`, {
          description: failed[0]?.error ?? 'Try again.',
        });
      } else {
        toast.success(`Listed ${source.name} in ${plural(results.length, 'draft')}`, {
          description: 'Publish each factory to give its runs the files.',
        });
      }
      setAdding(false);
      setPicked([]);
      await client.invalidateQueries({ queryKey: keys.knowledge(org.id) });
    },
  });

  return (
    <section aria-labelledby="readby-h" className={panel}>
      <div className="flex items-baseline justify-between gap-3 px-5 py-3">
        <h2 id="readby-h" className="m-0 text-heading">
          Read by
        </h2>
      </div>
      {readers.length === 0 ? (
        <p className="m-0 border-t border-line px-5 py-4 text-body text-muted">No factory lists it yet.</p>
      ) : (
        <table className="w-full border-collapse text-body">
          <caption className="sr-only">Factories and agents that read {source.name}</caption>
          <thead>
            <tr className="text-left text-secondary text-muted [&>th]:border-t [&>th]:border-line [&>th]:px-5 [&>th]:py-2 [&>th]:font-control">
              <th scope="col">Factory</th>
              <th scope="col">Agents</th>
            </tr>
          </thead>
          <tbody>
            {readers.map((r) => (
              <tr
                key={r.id}
                className="animate-row-in align-top [&>td]:border-t [&>td]:border-line [&>td]:px-5 [&>td]:py-3"
              >
                <td translate="no" className="break-words">
                  {r.name}
                </td>
                <td className="break-words">{readerScope(r)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {manage && rest.length > 0 ? (
        <div className="flex flex-col border-t border-line">
          {adding ? (
            <div className="flex animate-fade-in flex-col">
              <FactoryCheckboxes factories={rest} checked={picked} onChange={setPicked} disabled={add.isPending} />
              <div className="flex justify-end gap-2 border-t border-line p-3">
                <Button variant="white" className="h-11 md:h-8.5" onClick={() => setAdding(false)}>
                  Cancel
                </Button>
                <Button
                  variant="primary"
                  className="h-11 md:h-8.5"
                  disabled={picked.length === 0 || add.isPending}
                  onClick={() => add.mutate()}
                >
                  {add.isPending ? 'Adding…' : 'Add to drafts'}
                </Button>
              </div>
            </div>
          ) : (
            <button
              type="button"
              onClick={() => setAdding(true)}
              className={cn(
                'flex min-h-11 items-center gap-2 px-5 py-3 text-left text-body text-ink transition-colors duration-(--duration-quick) hover:bg-panel',
              )}
            >
              <Plus size={16} strokeWidth={1.75} aria-hidden="true" />
              Add to a factory
            </button>
          )}
        </div>
      ) : null}
    </section>
  );
}

function UploadCard({ source }: { source: KnowledgeSource }) {
  const org = useOrg();
  const uploader = useUploader(org.id);
  const id = useId();
  const over = uploader.rows.some((r) => r.state === 'uploaded' || r.state === 'refused');
  return (
    <section aria-labelledby={id} className={card}>
      <div className="flex items-baseline justify-between gap-3">
        <h2 id={id} className="m-0 text-heading">
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

/** Title, read-when line, schedule and what a source reads; the name and kind never change. */
function SettingsCard({ source: s, manage }: { source: KnowledgeSource; manage: boolean }) {
  const org = useOrg();
  const client = useQueryClient();
  const id = useId();
  // The reader is the one the config names, else what its keys say.
  const reads = s.kind !== 'upload' && s.kind !== 'live';
  const reader =
    s.kind === 'repository'
      ? 'gitdocs'
      : s.kind === 'confluence'
        ? 'confluence'
        : reads
          ? readerOf(undefined, s.config)
          : undefined;
  const edits = useEdits({
    title: s.title,
    readWhen: s.read_when,
    every: s.sync_every_seconds,
    config: s.config,
    secret: s.secret ?? '',
  });
  const { title, readWhen, every, config, secret } = edits.value;
  const [submitted, setSubmitted] = useState(false);
  const changedConfig = JSON.stringify(trimmed(config)) !== JSON.stringify(trimmed(s.config));
  const changedSecret = reads && secret !== '' && secret !== (s.secret ?? '');
  const dirty =
    title.trim() !== s.title ||
    readWhen.trim() !== s.read_when ||
    every !== s.sync_every_seconds ||
    changedConfig ||
    changedSecret;
  const titleError = title.trim() === '' ? 'A title is required.' : undefined;
  const errors = configErrors(reader, config);

  const save = useMutation({
    mutationFn: () =>
      updateKnowledgeSource(org.id, s.name, {
        ...(title.trim() !== s.title ? { title: title.trim() } : {}),
        ...(readWhen.trim() !== s.read_when ? { read_when: readWhen.trim() } : {}),
        ...(every !== s.sync_every_seconds ? { sync_every_seconds: every } : {}),
        ...(changedConfig ? { config: trimmed(config) } : {}),
        ...(changedSecret ? { secret } : {}),
      }),
    onSuccess: async (updated) => {
      client.setQueryData(keys.knowledgeSource(org.id, updated.name), updated);
      edits.reset();
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
      <h2 id={id} className="m-0 text-heading">
        Settings
      </h2>
      <Field
        label="Title"
        error={submitted ? titleError : undefined}
        help="What people see on Knowledge and in the builder."
      >
        <Input value={title} onChange={(e) => edits.set('title', e.target.value)} maxLength={120} autoComplete="off" />
      </Field>
      <Field
        label="When should agents read it?"
        help={`Every run’s prompt shows this line. ${readWhen.length} of 160.`}
      >
        <Textarea
          value={readWhen}
          onChange={(e) => edits.set('readWhen', e.target.value.slice(0, 160))}
          rows={2}
          maxLength={160}
        />
      </Field>
      <Field label="Name" help="Its folder in every run: /knowledge/name. It never changes.">
        <Input value={s.name} readOnly translate="no" className="font-mono md:text-body" />
      </Field>
      {reads ? (
        <>
          <SecretField
            value={secret}
            onChange={(name) => edits.set('secret', name)}
            help={
              s.secret
                ? 'The next sync reads with the new secret. Open to all factories.'
                : `Reads with ${s.connection ?? 'an earlier setup'} until you pick a secret. Open to all factories.`
            }
          />
          <ConfigFields
            reader={reader}
            secret={secret}
            config={config}
            errors={submitted ? errors : {}}
            onChange={(c) => edits.set('config', c)}
          />
          <Field label="Sync every" help="At least 15 minutes. Only changed documents are fetched again.">
            <NativeSelect value={every} onChange={(e) => edits.set('every', Number(e.target.value))}>
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
        <h2 className="m-0 text-heading">Delete {s.title}</h2>
        <p id={reasonId} className="m-0 text-body leading-normal text-muted">
          {users.length > 0
            ? `${namesText(users)} read it, so Goal refuses until ${users.length === 1 ? 'its definition no longer lists' : 'their definitions no longer list'} it. Then you type the name to confirm.`
            : `Its files and snapshots go with it${s.kind === 'upload' ? ', and the uploaded files' : ''}. You type the name to confirm.`}
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
        description={`${plural(s.latest_snapshot?.files ?? 0, 'file')} and ${s.latest_snapshot?.files === 1 ? 'its' : 'their'} snapshots go with it. This can’t be undone.`}
        confirmLabel="Delete source"
        pendingLabel="Deleting…"
        tone="danger"
        confirmText={s.name}
        pending={remove.isPending}
        onConfirm={() => remove.mutate()}
      >
        {remove.error ? (
          <p role="alert" className="m-0 text-body text-rework-ink">
            {refusalText(remove.error)}
          </p>
        ) : null}
      </ConfirmDialog>
    </section>
  );
}

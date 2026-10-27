// New source (spec 4 §10, canvas "Knowledge-Source-New"): choose a kind (?kind=), then what it
// needs. Upload: a title and name, and files dropped here go up once the source exists. Synced: a
// connection whose kind can sync, what of it to read (a repository's paths, Confluence spaces)
// and how often. Live: a connection that runs an MCP server; nothing is stored. Add source opens
// the new source's page, which says what happens next.
import { useMutation, useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { Info } from 'lucide-react';
import { type FormEvent, useId, useState } from 'react';
import { toast } from 'sonner';
import { ApiError } from '../../api/client';
import { createKnowledgeSource } from '../../api/knowledge';
import { connectionKindsQuery, connectionsQuery, keys, knowledgeSourcesQuery } from '../../api/queries';
import type { Connection, ConnectionKind, KnowledgeSourceKind } from '../../api/types';
import { TopBarSlot } from '../../app/top-bar';
import { sourceKinds } from '../../shared/knowledge';
import { useOrg, useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { cn } from '../../ui/cn';
import { Field, Input, NativeSelect } from '../../ui/field';
import { descriptionClass, PageHeader } from '../../ui/page-header';
import { type Config, ConfigFields, configErrors, readerOf, trimmed } from './config-fields';
import { DropZone, UploadRows, useUploader } from './upload';
import { nameFor, refusalText, sourceName, syncChoices } from './words';

const card = 'flex min-w-0 flex-col gap-4 rounded-card border border-line bg-card p-4 md:p-5';

const kindText: Record<KnowledgeSourceKind, (org: string) => string> = {
  upload: (org) =>
    `Drop files. Text, Markdown, HTML, JSON or YAML, each up to goald’s upload limit (25 MiB by default). Stored in ${org}’s bucket and indexed.`,
  synced: () =>
    'A connection that can sync: a repository’s docs, or a Confluence space. Goal copies the text on a schedule, indexes it and links back.',
  live: () =>
    'A connection that searches by itself, such as a Slack MCP server. Nothing is stored; factories listing it get its tools.',
};

export function NewSourcePage() {
  const org = useOrg();
  const search = useSearch({ from: '/o/$org/_org/knowledge/new' });
  const navigate = useNavigate({ from: '/o/$org/knowledge/new' });
  const manage = useOrgHolds('knowledge:manage');
  const kind = search.kind ?? 'upload';
  const id = useId();

  return (
    <>
      <TopBarSlot crumb="New source" />
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
          <li aria-current="page">New source</li>
        </ol>
      </nav>
      <PageHeader
        title="New source"
        description={
          <p className={cn(descriptionClass, 'max-w-[680px]')}>
            Add text agents can search: files you upload, or a system Goal reaches through one of {org.name}’s
            connections. Factories add a source in their Definition.
          </p>
        }
      />
      {manage ? (
        <div className="flex max-w-[980px] flex-col gap-4">
          <section aria-labelledby={id} className={card}>
            <fieldset className="m-0 min-w-0 border-0 p-0">
              <legend id={id} className="mb-3 p-0 text-lg font-medium">
                Kind
              </legend>
              <div className="grid gap-3 @[720px]:grid-cols-3">
                {(['upload', 'synced', 'live'] as const).map((k) => {
                  const { label, icon: Icon } = sourceKinds[k];
                  return (
                    <label
                      key={k}
                      className={cn(
                        'flex min-w-0 cursor-pointer items-start gap-3 rounded-control border bg-card p-4 transition-colors duration-200',
                        k === kind
                          ? 'border-ink shadow-[inset_0_0_0_1px_var(--color-ink)]'
                          : 'border-line hover:border-rule',
                      )}
                    >
                      <input
                        type="radio"
                        name="kind"
                        value={k}
                        checked={k === kind}
                        onChange={() => void navigate({ search: { kind: k }, replace: true })}
                        className="m-0 mt-0.5 size-5 shrink-0 cursor-pointer appearance-none rounded-full border border-rule bg-card transition-[border-width,border-color] duration-200 checked:border-[6px] checked:border-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-run"
                      />
                      <span className="flex min-w-0 flex-col gap-1">
                        <span className="inline-flex items-center gap-2 text-[15px] font-medium">
                          <Icon size={16} strokeWidth={1.5} aria-hidden="true" />
                          {label}
                        </span>
                        <span className="text-sm leading-normal text-muted">{kindText[k](org.name)}</span>
                      </span>
                    </label>
                  );
                })}
              </div>
            </fieldset>
          </section>
          <SourceForm key={kind} kind={kind} />
        </div>
      ) : (
        <Callout tone="note">Owners and admins of {org.name} add knowledge sources.</Callout>
      )}
    </>
  );
}

/** A connection's kind: the one a synced source's reader comes from, or whose tools a live source gets. */
function useConnections(kind: KnowledgeSourceKind) {
  const org = useOrg();
  const { data: connections } = useSuspenseQuery(connectionsQuery(org.id));
  const { data: kinds } = useSuspenseQuery(connectionKindsQuery(org.id));
  const kindOf = (c: Connection): ConnectionKind | undefined => kinds.find((k) => k.name === c.kind);
  const fits = (c: Connection) => (kind === 'synced' ? Boolean(kindOf(c)?.sync) : kindOf(c)?.modes.includes('mcp'));
  return { usable: connections.filter(fits), kindOf };
}

function SourceForm({ kind }: { kind: KnowledgeSourceKind }) {
  const org = useOrg();
  const client = useQueryClient();
  const navigate = useNavigate();
  const mayConnect = useOrgHolds('connection:manage');
  const taken = (useQuery(knowledgeSourcesQuery(org.id)).data ?? []).map((s) => s.name);
  const { usable, kindOf } = useConnections(kind);
  const uploader = useUploader(org.id);
  const [title, setTitle] = useState('');
  const [name, setName] = useState('');
  const [named, setNamed] = useState(false);
  const [connection, setConnection] = useState(kind === 'upload' ? '' : (usable[0]?.name ?? ''));
  const [config, setConfig] = useState<Config>({});
  const [every, setEvery] = useState(3600);
  const [submitted, setSubmitted] = useState(false);
  const id = useId();

  const chosen = usable.find((c) => c.name === connection);
  const reader = kind === 'synced' && chosen ? readerOf(kindOf(chosen)?.sync, {}) : undefined;
  const errors = kind === 'synced' ? configErrors(reader, config) : {};
  const shownName = named ? name : nameFor(title, taken);
  const titleError = title.trim() === '' ? 'A title is required.' : undefined;
  const nameError = !sourceName.test(shownName)
    ? '3 to 40 lowercase letters, digits and dashes, starting with a letter.'
    : taken.includes(shownName)
      ? `${org.name} already has a source named ${shownName}.`
      : undefined;
  const connectionError = kind !== 'upload' && !chosen ? 'Choose a connection.' : undefined;

  const create = useMutation({
    mutationFn: () =>
      createKnowledgeSource(org.id, {
        name: shownName,
        title: title.trim(),
        kind,
        ...(kind === 'upload' ? {} : { connection }),
        ...(kind === 'synced' ? { config: trimmed(config), sync_every_seconds: every } : {}),
      }),
    onSuccess: async (created) => {
      client.setQueryData(keys.knowledgeSource(org.id, created.name), created);
      if (kind === 'upload') {
        const { refused } = await uploader.upload(created.name, uploader.rows);
        if (refused > 0) {
          toast.error(`${refused === 1 ? 'A file was' : `${refused} files were`} refused`, {
            description: 'The source is added; upload them again from its page once they fit.',
          });
        }
      }
      await client.invalidateQueries({ queryKey: keys.knowledge(org.id) });
      await navigate({
        to: '/o/$org/knowledge/$source',
        params: { org: org.slug, source: created.name },
        search: { created: true },
      });
    },
  });
  const takenByGoald = create.error instanceof ApiError && create.error.code === 'knowledge-source-name-taken';

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setSubmitted(true);
    if (titleError || nameError || connectionError || Object.keys(errors).length > 0 || create.isPending) return;
    create.mutate();
  };

  const heading = { upload: 'Upload', synced: 'Synced from a connection', live: 'Live from a connection' }[kind];
  const noConnection =
    kind === 'synced'
      ? `${org.name} has no connection whose kind can sync yet: a GitHub, GitLab or Confluence connection.`
      : `${org.name} has no connection that runs an MCP server yet.`;

  return (
    <form onSubmit={submit} noValidate aria-labelledby={id} className="flex flex-col gap-4">
      <section className={card}>
        <h2 id={id} className="m-0 text-lg font-medium">
          {heading}
        </h2>
        {kind !== 'upload' && usable.length === 0 ? (
          <Callout tone="note">
            {noConnection}{' '}
            {mayConnect ? (
              <Link
                to="/o/$org/connections/new"
                params={{ org: org.slug }}
                className="text-ink underline underline-offset-3"
              >
                New connection
              </Link>
            ) : null}
          </Callout>
        ) : null}
        <div className="grid grid-cols-1 gap-x-5 gap-y-4 @[640px]:grid-cols-2">
          {kind === 'upload' ? null : (
            <Field
              label="Connection"
              error={submitted ? connectionError : undefined}
              help={
                <>
                  {kind === 'synced'
                    ? 'Only connections whose kind can sync.'
                    : 'Only connections that run an MCP server.'}{' '}
                  {mayConnect ? (
                    <Link
                      to="/o/$org/connections/new"
                      params={{ org: org.slug }}
                      className="text-ink underline underline-offset-3"
                    >
                      New connection
                    </Link>
                  ) : null}
                </>
              }
            >
              <NativeSelect
                value={connection}
                onChange={(e) => {
                  setConnection(e.target.value);
                  setConfig({});
                }}
              >
                {usable.length === 0 ? <option value="">None yet</option> : null}
                {usable.map((c) => (
                  <option key={c.id} value={c.name}>
                    {c.name}, {kindOf(c)?.title ?? c.kind}
                  </option>
                ))}
              </NativeSelect>
            </Field>
          )}
          {kind === 'synced' ? (
            <ConfigFields
              reader={reader}
              connection={connection}
              config={config}
              errors={submitted ? errors : {}}
              onChange={setConfig}
            />
          ) : null}
          <Field label="Title" error={submitted ? titleError : undefined} help="What people see, such as Infra docs.">
            <Input value={title} onChange={(e) => setTitle(e.target.value)} maxLength={120} autoComplete="off" />
          </Field>
          <Field
            label="Name"
            error={takenByGoald ? 'Taken' : submitted || taken.includes(shownName) ? nameError : undefined}
            help="How definitions list it. It never changes."
          >
            <Input
              value={shownName}
              onChange={(e) => {
                setNamed(true);
                setName(e.target.value);
              }}
              maxLength={40}
              autoComplete="off"
              autoCapitalize="off"
              spellCheck={false}
              translate="no"
              className="font-mono md:text-sm"
            />
          </Field>
          {kind === 'synced' ? (
            <Field label="Sync every" help="At least 15 minutes. Only changed documents are fetched again.">
              <NativeSelect value={every} onChange={(e) => setEvery(Number(e.target.value))}>
                {syncChoices.map((c) => (
                  <option key={c.seconds} value={c.seconds}>
                    {c.label}
                  </option>
                ))}
              </NativeSelect>
            </Field>
          ) : null}
        </div>
        {kind === 'upload' ? (
          <div className="flex flex-col gap-2">
            <DropZone onFiles={(files) => void uploader.add(files)} disabled={create.isPending} />
            <UploadRows rows={uploader.rows} onRemove={create.isPending ? undefined : uploader.remove} />
            <p className="m-0 text-[13px] text-muted">
              The files go up when you add the source. You can add more on its page later.
            </p>
          </div>
        ) : null}
      </section>

      {create.error && !takenByGoald ? (
        <div role="alert">
          <Callout tone="rework">{refusalText(create.error)}</Callout>
        </div>
      ) : null}
      {kind !== 'upload' ? (
        <p className="m-0 flex items-start gap-2.5 text-sm leading-normal text-ink-soft">
          <Info size={16} strokeWidth={1.75} aria-hidden="true" className="mt-0.5 shrink-0 text-muted" />
          {kind === 'synced'
            ? 'Goal indexes the source with the connection’s access. Every factory that lists it can find anything that access sees.'
            : 'Agents of factories that list it call the connection’s tools with the connection’s access.'}
        </p>
      ) : null}
      <div className="flex flex-wrap items-center justify-between gap-2 border-t border-line pt-4">
        <Button asChild variant="white" className="h-11 md:h-10">
          <Link to="/o/$org/knowledge" params={{ org: org.slug }}>
            Cancel
          </Link>
        </Button>
        <Button type="submit" variant="primary" className="h-11 md:h-10" disabled={create.isPending || uploader.busy}>
          {create.isPending || uploader.busy ? 'Adding…' : 'Add source'}
        </Button>
      </div>
    </form>
  );
}

// New source (spec 4 §10, canvas "Knowledge-Source-New"): choose a kind (?kind=), then what it
// needs. Upload: a title and name, and files dropped here go up once the source exists. Synced: a
// reader, the secret it reads with (open to all factories), what of it to read (a repository's paths,
// Confluence spaces) and how often. Live: a server that runs an MCP server; nothing is stored. Add source opens
// the new source's page, which says what happens next.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
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
import { Field, Input, NativeSelect } from '../../ui/field';
import { RadioCards } from '../../ui/radio-cards';
import { type Config, ConfigFields, configErrors, readerChoices, readerOf, trimmed } from './config-fields';
import { SecretField } from './secret-field';
import { DropZone, UploadRows, useUploader } from './upload';
import { nameFor, refusalText, sourceName, syncChoices } from './words';

const card = 'flex min-w-0 flex-col gap-4 rounded-card border border-line bg-card p-4 md:p-5';

const kindText: Record<KnowledgeSourceKind, (org: string) => string> = {
  upload: (org) =>
    `Drop files. Text, Markdown, HTML, JSON or YAML, each up to goald’s upload limit (25 MiB by default). Stored in ${org}’s bucket and indexed.`,
  synced: () =>
    'A repository’s docs or a Confluence space, read with a secret. Goal copies the text on a schedule, indexes it and links back.',
  live: () =>
    'A server that searches by itself, such as a Slack MCP server. Nothing is stored; factories listing it get its tools.',
  repository: () => 'A repository’s Markdown and text files, read with a secret and kept in sync.',
  confluence: () => 'A Confluence site’s pages, read with a secret and kept in sync.',
};

export function NewSourcePage() {
  const org = useOrg();
  const search = useSearch({ from: '/o/$org/_org/knowledge/new' });
  const navigate = useNavigate({ from: '/o/$org/knowledge/new' });
  const manage = useOrgHolds('knowledge:manage');
  const kind = search.kind ?? 'upload';

  return (
    <>
      <TopBarSlot crumb="New source" />
      <header className="flex flex-col gap-1">
        <h1 className="m-0 text-page-title">New source</h1>
        <p className="m-0 max-w-[680px] text-body text-muted">
          Add text agents can search: files you upload, or a system Goal reaches with one of {org.name}’s secrets.
          Factories add a source in their Definition.
        </p>
      </header>
      {manage ? (
        <div className="flex max-w-[980px] flex-col gap-4">
          <div className="flex flex-col gap-2">
            <span aria-hidden="true" className="text-secondary font-control text-ink">
              Kind
            </span>
            <RadioCards
              legend="Kind"
              showLegend={false}
              name="kind"
              value={kind}
              onChange={(k) => void navigate({ search: { kind: k }, replace: true })}
              gridClassName="@[720px]:grid-cols-3"
              options={(['upload', 'synced', 'live'] as const).map((k) => {
                const { label, icon: Icon } = sourceKinds[k];
                return {
                  value: k,
                  label,
                  icon: <Icon size={16} strokeWidth={1.5} />,
                  description: kindText[k](org.name),
                };
              })}
            />
          </div>
          <SourceForm key={kind} kind={kind} />
        </div>
      ) : (
        <Callout tone="note">Owners and admins of {org.name} add knowledge sources.</Callout>
      )}
    </>
  );
}

/** The servers a live source can read: connections that run an MCP server. A synced source needs none. */
function useServers(kind: KnowledgeSourceKind) {
  const org = useOrg();
  const live = kind === 'live';
  const connections = useQuery({ ...connectionsQuery(org.id), enabled: live }).data ?? [];
  const kinds = useQuery({ ...connectionKindsQuery(org.id), enabled: live }).data ?? [];
  const kindOf = (c: Connection): ConnectionKind | undefined => kinds.find((k) => k.name === c.kind);
  return { usable: live ? connections.filter((c) => kindOf(c)?.modes.includes('mcp')) : [], kindOf };
}

function SourceForm({ kind }: { kind: KnowledgeSourceKind }) {
  const org = useOrg();
  const client = useQueryClient();
  const navigate = useNavigate();
  const taken = (useQuery(knowledgeSourcesQuery(org.id)).data ?? []).map((s) => s.name);
  const { usable, kindOf } = useServers(kind);
  const uploader = useUploader(org.id);
  const [title, setTitle] = useState('');
  const [name, setName] = useState('');
  const [named, setNamed] = useState(false);
  const [connection, setConnection] = useState(usable[0]?.name ?? '');
  const [secret, setSecret] = useState('');
  const [readerName, setReaderName] = useState('gitdocs');
  const [config, setConfig] = useState<Config>({});
  const [every, setEvery] = useState(3600);
  const [submitted, setSubmitted] = useState(false);
  const id = useId();

  const chosen = usable.find((c) => c.name === connection);
  const reader = kind === 'synced' ? readerOf(readerName, {}) : undefined;
  const errors = kind === 'synced' ? configErrors(reader, config) : {};
  const shownName = named ? name : nameFor(title, taken);
  const titleError = title.trim() === '' ? 'A title is required.' : undefined;
  const nameError = !sourceName.test(shownName)
    ? '3 to 40 lowercase letters, digits and dashes, starting with a letter.'
    : taken.includes(shownName)
      ? `${org.name} already has a source named ${shownName}.`
      : undefined;
  const connectionError =
    kind === 'live' && !chosen ? 'Choose a server.' : kind === 'synced' && secret === '' ? 'Pick a secret.' : undefined;

  const create = useMutation({
    mutationFn: () =>
      createKnowledgeSource(org.id, {
        name: shownName,
        title: title.trim(),
        kind,
        ...(kind === 'live' ? { connection } : {}),
        ...(kind === 'synced'
          ? { secret, config: { ...trimmed(config), reader: readerName }, sync_every_seconds: every }
          : {}),
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

  const heading = {
    upload: 'Upload',
    synced: 'Synced with a secret',
    live: 'Live from a server',
    repository: 'Repository',
    confluence: 'Confluence',
  }[kind];
  const noServer = `${org.name} has no MCP server a live source can read yet. Add one to an environment.`;

  return (
    <form onSubmit={submit} noValidate aria-labelledby={id} className="flex flex-col gap-4">
      <section className={card}>
        <h2 id={id} className="m-0 text-heading">
          {heading}
        </h2>
        {kind === 'live' && usable.length === 0 ? <Callout tone="note">{noServer}</Callout> : null}
        <div className="grid grid-cols-1 gap-x-5 gap-y-4 @[640px]:grid-cols-2">
          {kind === 'live' ? (
            <Field
              label="Server"
              error={submitted ? connectionError : undefined}
              help="Only servers that run an MCP server."
            >
              <NativeSelect
                value={connection}
                onChange={(e) => {
                  setConnection(e.target.value);
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
          ) : null}
          {kind === 'synced' ? (
            <>
              <Field label="Reads">
                <NativeSelect
                  value={readerName}
                  onChange={(e) => {
                    setReaderName(e.target.value);
                    setConfig({});
                  }}
                >
                  {readerChoices.map((r) => (
                    <option key={r.value} value={r.value}>
                      {r.label}
                    </option>
                  ))}
                </NativeSelect>
              </Field>
              <SecretField value={secret} onChange={setSecret} error={submitted ? connectionError : undefined} />
            </>
          ) : null}
          {kind === 'synced' ? (
            <ConfigFields
              reader={reader}
              secret={secret}
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
              className="font-mono md:text-body"
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
            <p className="m-0 text-secondary text-muted">
              The files go up when you add the source. You can add more on its page later.
            </p>
          </div>
        ) : null}
        <div className="flex flex-wrap items-center justify-between gap-2 border-t border-line pt-4">
          <Button asChild variant="white" className="h-11 md:h-8.5">
            <Link to="/o/$org/knowledge" params={{ org: org.slug }}>
              Cancel
            </Link>
          </Button>
          <Button
            type="submit"
            variant="primary"
            className="h-11 md:h-8.5"
            disabled={create.isPending || uploader.busy}
          >
            {create.isPending || uploader.busy ? 'Adding…' : 'Add source'}
          </Button>
        </div>
      </section>

      {create.error && !takenByGoald ? (
        <div role="alert">
          <Callout tone="rework">{refusalText(create.error)}</Callout>
        </div>
      ) : null}
      {kind !== 'upload' ? (
        <p className="m-0 flex items-start gap-2.5 text-body leading-normal text-ink-soft">
          <Info size={16} strokeWidth={1.75} aria-hidden="true" className="mt-0.5 shrink-0 text-muted" />
          {kind === 'synced'
            ? 'Goal indexes the source with the secret’s access. Every factory that lists it can find anything that access sees.'
            : 'Agents of factories that list it call the server’s tools with the server’s access.'}
        </p>
      ) : null}
    </form>
  );
}

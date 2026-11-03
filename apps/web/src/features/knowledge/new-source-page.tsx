// Add a source (ADR 0038 decision 12, canvas "Knowledge-New" and its Upload, Confluence and 768 px
// variants): one page. Pick where the files are (Upload, Repository or Confluence; the rest show as
// later), fill in that kind's settings, name it and say when agents should read it, and pick the
// factories that read it. The Check beside the form reads the source with these settings before
// anything is saved. Add source creates it and lists it in each picked factory's draft; it never
// publishes a factory. The kind is in the URL (?kind=) so a link opens the page on it.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { Globe, HardDrive, NotebookText } from 'lucide-react';
import { type FormEvent, useId, useState } from 'react';
import { toast } from 'sonner';
import { ApiError } from '../../api/client';
import { createKnowledgeSource } from '../../api/knowledge';
import { factoriesQuery, keys, knowledgeSourcesQuery } from '../../api/queries';
import type { KnowledgeCheckInput, KnowledgeSourceKind } from '../../api/types';
import { TopBarSlot } from '../../app/top-bar';
import { sourceKinds } from '../../shared/knowledge';
import { useOrg, useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { Field, Input, Textarea } from '../../ui/field';
import { RadioCards } from '../../ui/radio-cards';
import { SegmentedControl } from '../../ui/segmented-control';
import { CheckPanel } from './check-panel';
import { type Config, ConfigFields, configErrors, trimmed } from './config-fields';
import { listInDrafts } from './draft';
import { FactoryCheckboxes } from './factory-checkboxes';
import { SecretField } from './secret-field';
import { DropZone, UploadRows, useUploader } from './upload';
import { refusalText, sourceName } from './words';

const card = 'flex min-w-0 flex-col rounded-card border border-line bg-card';
const cardBody = 'flex flex-col gap-4 border-t border-line p-4 md:p-5';
const maxReadWhen = 160;

type NewKind = Extract<KnowledgeSourceKind, 'upload' | 'repository' | 'confluence'>;

const kinds: readonly { value: NewKind; text: string }[] = [
  { value: 'upload', text: 'Drop text, Markdown, HTML, JSON or YAML files.' },
  { value: 'repository', text: 'Folders of a GitHub or GitLab repository.' },
  { value: 'confluence', text: 'The pages of one or more spaces.' },
];

const later = [
  { value: 'website', label: 'Website', text: 'A docs site, page by page.', icon: Globe },
  { value: 'notion', label: 'Notion', text: 'Pages shared with Goal.', icon: NotebookText },
  { value: 'drive', label: 'Google Drive', text: 'A shared folder.', icon: HardDrive },
] as const;

const schedules = [
  { value: '3600', label: 'Every hour' },
  { value: '86400', label: 'Every day' },
] as const;

export function NewSourcePage() {
  const org = useOrg();
  const manage = useOrgHolds('knowledge:manage');
  const kind: NewKind = useSearch({ from: '/o/$org/_org/knowledge/new' }).kind ?? 'upload';

  return (
    <>
      <TopBarSlot crumb="New source" />
      <header className="flex flex-col gap-1">
        <h1 className="m-0 text-page-title">New source</h1>
        <p className="m-0 max-w-[680px] text-body text-muted">
          Point at the documents, say when agents should read them, and pick the factories that read them.
        </p>
      </header>
      {manage ? (
        <SourceForm key={kind} kind={kind} />
      ) : (
        <Callout tone="note">Owners and admins of {org.name} add knowledge sources.</Callout>
      )}
    </>
  );
}

function SourceForm({ kind }: { kind: NewKind }) {
  const org = useOrg();
  const client = useQueryClient();
  const navigate = useNavigate();
  const taken = (useQuery(knowledgeSourcesQuery(org.id)).data ?? []).map((s) => s.name);
  const factories = (useQuery(factoriesQuery(org.id)).data ?? []).filter((f) => !f.archived_at);
  const uploader = useUploader(org.id);
  const [name, setName] = useState('');
  const [readWhen, setReadWhen] = useState('');
  const [secret, setSecret] = useState('');
  const [config, setConfig] = useState<Config>({});
  const [every, setEvery] = useState('3600');
  const [picked, setPicked] = useState<string[]>([]);
  const [submitted, setSubmitted] = useState(false);
  const id = useId();

  const reader = kind === 'repository' ? 'gitdocs' : kind === 'confluence' ? 'confluence' : undefined;
  const errors = kind === 'upload' ? {} : configErrors(reader, config);
  const secretError = kind === 'confluence' && secret === '' ? 'Pick a secret.' : undefined;
  const nameError = !sourceName.test(name)
    ? '3 to 40 lowercase letters, digits and dashes, starting with a letter.'
    : taken.includes(name)
      ? `${org.name} already has a source named ${name}.`
      : undefined;
  const readWhenError = readWhen.trim() === '' ? 'Say when agents should read it.' : undefined;
  const valid = !nameError && !readWhenError && !secretError && Object.keys(errors).length === 0;

  const body = {
    name,
    kind,
    ...(kind === 'upload' ? {} : { config: trimmed(config) }),
    ...(secret ? { secret } : {}),
    read_when: readWhen.trim(),
  };
  // The Check reads the source as typed once its settings are complete; the name only has to be a name.
  const checkable = sourceName.test(name) && !secretError && Object.keys(errors).length === 0;
  const checkInput: KnowledgeCheckInput | null = checkable ? { ...body, factories: picked } : null;

  const create = useMutation({
    mutationFn: () =>
      createKnowledgeSource(org.id, {
        ...body,
        title: name,
        ...(kind === 'upload' ? {} : { sync_every_seconds: Number(every) }),
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
      // The source exists now, so the drafts that list it check out; each is written, none published.
      const results = await listInDrafts(
        org.id,
        factories.filter((f) => picked.includes(f.id)),
        created.name,
      );
      const failed = results.filter((r) => r.outcome === 'failed');
      if (failed.length > 0) {
        toast.error(`Couldn’t list ${created.name} in ${failed.map((r) => r.factory).join(', ')}`, {
          description: failed[0]?.error ?? 'Add it from the source page.',
        });
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
    if (!valid || create.isPending) return;
    create.mutate();
  };

  return (
    <form
      onSubmit={submit}
      noValidate
      aria-labelledby={id}
      className="grid items-start gap-4 @[1100px]:grid-cols-[minmax(0,1fr)_380px]"
    >
      <div className="flex min-w-0 flex-col gap-4">
        <section className={card}>
          <h2 id={id} className="m-0 px-5 py-3 text-heading">
            Where the files are
          </h2>
          <div className={cardBody}>
            <RadioCards
              legend="Where the files are"
              showLegend={false}
              name="kind"
              value={kind}
              onChange={(k) =>
                void navigate({
                  to: '/o/$org/knowledge/new',
                  params: { org: org.slug },
                  search: { kind: k },
                  replace: true,
                })
              }
              gridClassName="grid-cols-1 @[560px]:grid-cols-3"
              options={[
                ...kinds.map((k) => {
                  const { label, icon: Icon } = sourceKinds[k.value];
                  return { value: k.value, label, icon: <Icon size={16} strokeWidth={1.5} />, description: k.text };
                }),
                ...later.map((l) => ({
                  value: l.value as NewKind,
                  label: l.label,
                  icon: <l.icon size={16} strokeWidth={1.5} />,
                  description: l.text,
                  disabled: true,
                  badge: <Chip>Later</Chip>,
                })),
              ]}
            />
          </div>
        </section>

        <section className={card} aria-labelledby={`${id}-settings`}>
          <h2 id={`${id}-settings`} className="m-0 px-5 py-3 text-heading">
            {sourceKinds[kind].label}
          </h2>
          <div className={cardBody}>
            {kind === 'upload' ? (
              <div className="flex flex-col gap-2">
                <DropZone onFiles={(files) => void uploader.add(files)} disabled={create.isPending} />
                <UploadRows rows={uploader.rows} onRemove={create.isPending ? undefined : uploader.remove} />
                <p className="m-0 text-secondary text-muted">
                  The files go up when you add the source. You can add more on its page.
                </p>
              </div>
            ) : (
              <div className="grid grid-cols-1 gap-x-5 gap-y-4 @[640px]:grid-cols-2">
                <ConfigFields
                  reader={reader}
                  secret={secret}
                  config={config}
                  errors={submitted ? errors : {}}
                  onChange={setConfig}
                />
                <SecretField
                  value={secret}
                  onChange={setSecret}
                  error={submitted ? secretError : undefined}
                  help={kind === 'repository' ? 'Read access is enough. None for a public repository.' : undefined}
                />
              </div>
            )}
          </div>
        </section>

        <section className={card} aria-labelledby={`${id}-what`}>
          <h2 id={`${id}-what`} className="m-0 px-5 py-3 text-heading">
            What it is
          </h2>
          <div className={cardBody}>
            <Field
              label="Name"
              className="@[640px]:max-w-[50%]"
              error={takenByGoald ? 'Taken' : submitted || taken.includes(name) ? nameError : undefined}
              help={`Its folder in every run: /knowledge/${name || 'name'}. It never changes.`}
            >
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                maxLength={40}
                autoComplete="off"
                autoCapitalize="off"
                spellCheck={false}
                translate="no"
                className="font-mono md:text-body"
              />
            </Field>
            <Field
              label="When should agents read it?"
              error={submitted ? readWhenError : undefined}
              help={`Every run’s prompt shows this line, so say when to read it, not only what it is. ${readWhen.length} of ${maxReadWhen}.`}
            >
              <Textarea
                value={readWhen}
                onChange={(e) => setReadWhen(e.target.value.slice(0, maxReadWhen))}
                rows={2}
                maxLength={maxReadWhen}
              />
            </Field>
          </div>
        </section>

        <section className={card} aria-labelledby={`${id}-factories`}>
          <div className="flex flex-wrap items-baseline justify-between gap-x-3 px-5 py-3">
            <h2 id={`${id}-factories`} className="m-0 text-heading">
              Factories that read it
            </h2>
            <span className="text-secondary text-muted">Adds a line to each one’s draft; you publish it there</span>
          </div>
          <div className="border-t border-line">
            {factories.length === 0 ? (
              <p className="m-0 px-5 py-4 text-body text-muted">{org.name} has no factory yet.</p>
            ) : (
              <FactoryCheckboxes
                factories={factories}
                checked={picked}
                onChange={setPicked}
                disabled={create.isPending}
              />
            )}
          </div>
        </section>

        {kind !== 'upload' ? (
          <section className={card} aria-labelledby={`${id}-sync`}>
            <h2 id={`${id}-sync`} className="m-0 px-5 py-3 text-heading">
              Sync
            </h2>
            <div className={cardBody}>
              <SegmentedControl label="Sync" options={schedules} value={every} onChange={setEvery} />
            </div>
          </section>
        ) : null}

        {create.error && !takenByGoald ? (
          <div role="alert">
            <Callout tone="rework">{refusalText(create.error)}</Callout>
          </div>
        ) : null}

        <div className="flex flex-wrap items-center justify-between gap-3">
          <span className="text-secondary text-muted">
            The same from the CLI: <code className="font-mono">goal knowledge add</code>
          </span>
          <div className="flex gap-2">
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
        </div>
      </div>

      <div className="flex min-w-0 flex-col gap-4 @[1100px]:sticky @[1100px]:top-4">
        <CheckPanel input={checkInput} />
      </div>
    </form>
  );
}

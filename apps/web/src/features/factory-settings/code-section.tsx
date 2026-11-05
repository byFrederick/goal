// Settings › Code: what the factory's runs may open (ADR 0041 decision 10, docs/design/screens/build/factory-settings.jpg).
// Each runtime.repositories entry (or the runtime.repository shorthand, as one write entry) shows its URL, its
// access, its token by name and its kept result with Test. Below: the agents that write, with a link to Agents,
// Add a repository, the branch names and the host's default branch. The API carries no default branch yet, so it
// stays "Not available yet". Add and Remove edit the draft; Save changes publishes it.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Plus } from 'lucide-react';
import { useState } from 'react';
import { factoryRepositoryTestsQuery } from '../../api/queries';
import { testFactoryRepository } from '../../api/repositories';
import type { FactoryRepositoryTest } from '../../api/types';
import type { BasicsSection, RepositoryEntry } from '../../shared/definition/model';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { Chip } from '../../ui/chip';
import { Field, Input } from '../../ui/field';
import { InfoTip } from '../../ui/info-tip';
import { Leaving, useLeaving } from '../../ui/leaving';
import { NotAvailable } from '../../ui/not-available';
import { Skeleton } from '../../ui/skeleton';
import { touchLinkWide } from '../../ui/touch-target';
import { freshName, RepositoriesField } from '../factory-editor/basics/repositories-field';
import { setBasics } from '../factory-editor/draft/ops';
import { useDraftSession, useSection, useViewOnly } from '../factory-editor/draft/use-draft';
import { secretRef } from '../factory-editor/environments/env-file';
import { SettingsGroup } from './settings-group';

const accessLabel = { read: 'Read only', write: 'Read and write' } as const;

/** A URL as the kept results name it: host/owner/name, lower case, no scheme, no .git, no trailing slash. */
const keptUrl = (url: string) =>
  url
    .trim()
    .replace(/^https?:\/\//i, '')
    .replace(/\/+$/, '')
    .replace(/\.git$/, '')
    .toLowerCase();

/** The entries the section shows: runtime.repositories, or the runtime.repository shorthand as one write entry. */
function entriesOf(basics: BasicsSection): { entries: RepositoryEntry[]; shorthand: boolean } {
  if (basics.repositories && basics.repositories.length > 0) return { entries: basics.repositories, shorthand: false };
  const repository = basics.repository;
  if (repository?.url) {
    const token = repository.token;
    return {
      entries: [{ name: 'repo', url: repository.url, access: 'write', ...(token ? { token } : {}) }],
      shorthand: true,
    };
  }
  return { entries: [], shorthand: false };
}

/** The chip of a result: ok when the token does what the access asks, quiet when the host did not answer. */
function ResultChip({ result }: { result: FactoryRepositoryTest }) {
  const tone = !result.tested ? 'quiet' : result.ok ? 'ok' : 'rework';
  return (
    <Chip tone={tone} dot>
      {result.summary}
    </Chip>
  );
}

function EntryRow({
  entry,
  shorthand,
  kept,
  loading,
  onRemove,
}: {
  entry: RepositoryEntry;
  shorthand: boolean;
  kept: FactoryRepositoryTest | undefined;
  loading: boolean;
  onRemove: () => void;
}) {
  const org = useOrg();
  const factory = useFactory();
  const viewOnly = useViewOnly();
  const queryClient = useQueryClient();
  const secret = secretRef(entry.token ?? '') ?? (entry.token || undefined);
  const test = useMutation({
    mutationFn: () =>
      testFactoryRepository(org.id, factory.id, { url: entry.url, secret: secret ?? '', access: entry.access }),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: factoryRepositoryTestsQuery(org.id, factory.id).queryKey }),
  });
  const { leaving, leave } = useLeaving(onRemove);
  const result = test.data ?? kept;
  const shown = entry.url.replace(/^https?:\/\//, '');
  return (
    <li className="min-w-0 list-none motion-safe:animate-row-in motion-reduce:animate-fade-in">
      <Leaving leaving={leaving}>
        <div className="flex min-w-0 flex-col gap-2 rounded-control border border-line bg-card p-3">
          <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
            <span className="min-w-0 flex-1 truncate text-body" title={entry.url}>
              {shown}
            </span>
            <Chip>{accessLabel[entry.access]}</Chip>
          </div>
          <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2">
            <Skeleton ready={!loading} placeholder={<div className="h-5.5 w-40 rounded-control bg-shell" />}>
              {result ? (
                <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
                  <ResultChip result={result} />
                  {result.warning || result.note ? (
                    <span className="min-w-0 text-secondary text-muted">{result.warning ?? result.note}</span>
                  ) : null}
                </div>
              ) : (
                <Chip tone="quiet">Not tested</Chip>
              )}
            </Skeleton>
            {secret ? (
              <span className="min-w-0 max-w-full truncate font-mono text-meta text-muted" title={secret}>
                {secret}
              </span>
            ) : (
              <span className="text-secondary text-muted">No token</span>
            )}
            {viewOnly ? null : (
              <span className="ml-auto flex items-center gap-1">
                {secret ? (
                  <Button variant="white" size="sm" disabled={test.isPending} onClick={() => test.mutate()}>
                    {test.isPending ? 'Testing' : 'Test'}
                  </Button>
                ) : null}
                {shorthand ? null : (
                  <Button variant="ghost" size="sm" onClick={leave}>
                    Remove
                  </Button>
                )}
              </span>
            )}
          </div>
          {test.isError ? (
            <span className="text-secondary text-rework-ink">The test did not run. Try again.</span>
          ) : null}
        </div>
      </Leaving>
    </li>
  );
}

/** Agents whose effective access is write: an agent without `code` writes when the factory has a write entry. */
function writers(entries: RepositoryEntry[], agents: { name: string; code?: string }[]) {
  const writes = entries.some((e) => e.access === 'write');
  const names = agents.filter((a) => (a.code ?? (writes ? 'write' : 'read')) === 'write').map((a) => a.name);
  const allWrite = agents.every((a) => a.code !== 'read');
  return { names, all: allWrite && writes };
}

export function CodeSection() {
  const org = useOrg();
  const factory = useFactory();
  const params = useFactoryParams();
  const basics = useSection('basics');
  const agents = useSection('agents').agents ?? [];
  const viewOnly = useViewOnly();
  const session = useDraftSession();
  const [editing, setEditing] = useState(false);
  const { entries, shorthand } = entriesOf(basics);
  const kept = useQuery({ ...factoryRepositoryTestsQuery(org.id, factory.id), enabled: entries.length > 0 });
  const keptByUrl = new Map((kept.data ?? []).map((t) => [keptUrl(t.url), t]));
  const loading = entries.length > 0 && kept.isPending;
  const who = writers(entries, agents);
  const stored = basics.repositories ?? [];
  const edit = (next: RepositoryEntry[]) =>
    session.edit(setBasics(basics, 'repositories', next.length > 0 ? next : undefined));
  const add = () => {
    if (shorthand) {
      // Publish refuses runtime.repository together with runtime.repositories, so the shorthand moves into the list.
      const { repository, ...rest } = basics;
      const token = repository?.token;
      const first: RepositoryEntry = {
        name: 'repo',
        url: repository?.url ?? '',
        access: 'write',
        ...(token ? { token } : {}),
        default: true,
      };
      session.edit(setBasics(rest, 'repositories', [first, { name: freshName([first]), url: '', access: 'read' }]));
    } else {
      edit([...stored, { name: freshName(stored), url: '', access: 'read' }]);
    }
    setEditing(true);
  };

  return (
    <SettingsGroup
      id="code"
      title="Code"
      footer={
        <div className="flex min-w-0 flex-col gap-4">
          <div className="flex min-w-0 flex-col gap-1">
            <span className="flex items-center gap-1 text-sm font-medium">
              Agents that write
              <InfoTip about="Agents that write" className="max-md:size-11">
                Agents whose code is write push, open pull requests and merge in the repositories that write. An agent
                without a setting writes when the factory has a write repository.
              </InfoTip>
            </span>
            <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
              <span className="min-w-0 text-body">
                {entries.length === 0
                  ? 'None, no code'
                  : who.all
                    ? 'All agents'
                    : who.names.length > 0
                      ? who.names.join(', ')
                      : 'No agent'}
              </span>
              <Link
                to="/o/$org/f/$factory/agents"
                params={params}
                className={`${touchLinkWide} font-control text-secondary`}
              >
                Agents
              </Link>
            </div>
          </div>
          <Field label="Branch names" help="Each item's branch, in every repository it changes.">
            <Input readOnly value="goal/<number>-<slug>" className="font-mono" />
          </Field>
          <Field label="Default branch" help="Pull requests open against the host's default branch.">
            <NotAvailable label="Default branch">
              <Input readOnly aria-label="Default branch" placeholder="Not available yet" value="" />
            </NotAvailable>
          </Field>
        </div>
      }
    >
      <div className="flex min-w-0 flex-col gap-3">
        <span className="flex items-center gap-1 text-sm font-medium">
          Repositories
          <InfoTip about="Secrets and webhooks" className="max-md:size-11">
            Creating a secret or opening a webhook needs an owner or admin. Ask one if the token you need is not in the
            list.
          </InfoTip>
        </span>
        {editing ? (
          <>
            <RepositoriesField basics={basics} shorthand={shorthand} />
            <Button variant="white" size="sm" className="self-start" onClick={() => setEditing(false)}>
              Done
            </Button>
          </>
        ) : (
          <>
            {entries.length === 0 ? (
              <p className="m-0 text-body text-muted">No code. Agents work through their tools.</p>
            ) : (
              <ul className="m-0 flex min-w-0 list-none flex-col gap-3 p-0">
                {entries.map((entry) => (
                  <EntryRow
                    key={`${entry.name}:${entry.url}`}
                    entry={entry}
                    shorthand={shorthand}
                    kept={keptByUrl.get(keptUrl(entry.url))}
                    loading={loading}
                    onRemove={() => edit(stored.filter((e) => e !== stored.find((s) => s.name === entry.name)))}
                  />
                ))}
              </ul>
            )}
            {viewOnly ? null : (
              <Button variant="white" size="sm" className="self-start" onClick={add}>
                <Plus size={14} strokeWidth={1.5} aria-hidden="true" />
                Add a repository
              </Button>
            )}
          </>
        )}
      </div>
    </SettingsGroup>
  );
}

// The repository (spec 4 §4.4, canvas Define-Basics): an https URL cloned into the box before the
// agent starts, and the organization connection git signs in with (a kind with a git block; none
// for a public repository). An empty URL without a connection means no repository: no checkout,
// no pull requests. A version published before phase 4 may still name an env: token; its fix
// (env-reference-removed) removes it.
import { useId } from 'react';
import type { Connection, ConnectionKind } from '../../../api/types';
import type { BasicsSection, Repository } from '../../../shared/definition/model';
import { Input, NativeSelect } from '../../../ui/field';
import { setBasics } from '../draft/ops';
import { useDraftSession } from '../draft/use-draft';
import { ProblemFixes } from '../frame/problem-fixes';
import { EditedMark } from '../frame/section-shell';
import { useProblemText } from '../frame/use-editor';

/** The repository with `patch` applied; none once it has neither a URL nor a connection. */
export function patchRepository(repository: Repository | null | undefined, patch: Repository): Repository | undefined {
  const next: Repository = { ...repository, ...patch };
  for (const key of ['url', 'connection'] as const) if (next[key] === '') delete next[key];
  return next.url || next.connection || next.token ? next : undefined;
}

export function RepositoryField({
  basics,
  connections,
  kindOf,
}: {
  basics: BasicsSection;
  connections: Connection[];
  kindOf: (c: Connection) => ConnectionKind | undefined;
}) {
  const session = useDraftSession();
  const help = useId();
  const repository = basics.repository ?? undefined;
  const urlProblem = useProblemText('/basics/repository/url');
  const gitConnections = connections.filter((c) => kindOf(c)?.modes.includes('git'));
  const chosen = repository?.connection ?? '';
  const known = chosen === '' || gitConnections.some((c) => c.name === chosen);
  const edit = (patch: Repository, coalesce?: string) =>
    session.edit(setBasics(basics, 'repository', patchRepository(repository, patch)), coalesce ? { coalesce } : {});

  return (
    <div className="col-span-full flex min-w-0 flex-col gap-1.5">
      <div className="flex min-h-6 flex-wrap items-center justify-between gap-x-3 gap-y-1">
        <span className="text-sm font-medium">
          Repository <span className="font-normal text-muted">optional</span>
        </span>
        <EditedMark section="basics" pointer="/basics/repository" label="the repository" />
      </div>
      <div className="grid grid-cols-1 gap-2 @[640px]:grid-cols-[minmax(0,1fr)_200px]">
        <Input
          type="url"
          inputMode="url"
          aria-label="Repository URL"
          aria-describedby={help}
          aria-invalid={urlProblem ? true : undefined}
          value={repository?.url ?? ''}
          onChange={(e) => edit({ url: e.target.value }, '/basics/repository/url')}
          onBlur={() => void session.flush()}
          placeholder="https://github.com/acme/goal"
          autoComplete="off"
          spellCheck={false}
        />
        <NativeSelect
          aria-label="Repository connection"
          aria-describedby={help}
          value={chosen}
          onChange={(e) => edit({ connection: e.target.value })}
        >
          <option value="">Public, no connection</option>
          {known ? null : <option value={chosen}>{chosen}</option>}
          {gitConnections.map((c) => (
            <option key={c.name} value={c.name}>
              {c.name}
            </option>
          ))}
        </NativeSelect>
      </div>
      {urlProblem ? <p className="m-0 text-[13px] leading-snug text-rework-ink">{urlProblem}</p> : null}
      <ProblemFixes pointer="/basics/repository/connection" />
      <ProblemFixes pointer="/basics/repository/token" />
      <p id={help} className="m-0 text-[13px] leading-snug text-muted">
        {repository?.url
          ? 'Cloned into the box before the agent starts. Agents can clone other repositories their connection’s token allows.'
          : 'Without one, agents work without a checkout and open no pull requests.'}
      </p>
    </div>
  );
}

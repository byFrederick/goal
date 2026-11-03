// The repository (ADR 0037): an https URL cloned into the box before the agent starts, and the secret
// whose token git signs in with, written as {{secret}} in runtime.repository.token (none for a public
// repository). An empty URL without a token means no repository: no checkout, no pull requests.
import { useId } from 'react';
import type { BasicsSection, Repository } from '../../../shared/definition/model';
import { Button } from '../../../ui/button';
import { Input } from '../../../ui/field';
import { setBasics } from '../draft/ops';
import { useDraftSession } from '../draft/use-draft';
import { secretRef } from '../environments/env-file';
import { NewSecretSheetFor } from '../environments/new-secret';
import { SecretSelect } from '../environments/parts';
import { ProblemFixes } from '../frame/problem-fixes';
import { EditedMark } from '../frame/section-shell';
import { useProblemText } from '../frame/use-editor';

/** The repository with `patch` applied; none once it has neither a URL nor a connection. */
export function patchRepository(repository: Repository | null | undefined, patch: Repository): Repository | undefined {
  const next: Repository = { ...repository, ...patch };
  for (const key of ['url', 'connection', 'token'] as const) if (next[key] === '') delete next[key];
  return next.url || next.connection || next.token ? next : undefined;
}

export function RepositoryField({ basics }: { basics: BasicsSection }) {
  const session = useDraftSession();
  const help = useId();
  const newSecret = NewSecretSheetFor({ name: 'github-token' });
  const repository = basics.repository ?? undefined;
  const urlProblem = useProblemText('/basics/repository/url');
  const token = secretRef(repository?.token ?? '') ?? (repository?.token || undefined);
  const edit = (patch: Repository, coalesce?: string) =>
    session.edit(setBasics(basics, 'repository', patchRepository(repository, patch)), coalesce ? { coalesce } : {});
  const pick = (name: string) => edit({ token: `{{${name}}}` });

  return (
    <div className="col-span-full flex min-w-0 flex-col gap-1.5">
      <div className="flex min-h-6 flex-wrap items-center justify-between gap-x-3 gap-y-1">
        <span className="text-sm font-medium">
          Repository <span className="font-normal text-muted">optional</span>
        </span>
        <EditedMark section="basics" pointer="/basics/repository" label="the repository" />
      </div>
      <div className="grid grid-cols-1 gap-2 @[640px]:grid-cols-[minmax(0,1fr)_220px]">
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
        <SecretSelect
          label="Repository token"
          value={token}
          onChange={pick}
          placeholder="Public, no token"
          withNew={() => newSecret.open(pick)}
          className="w-full"
        />
      </div>
      {newSecret.sheet}
      {urlProblem ? <p className="m-0 text-[13px] leading-snug text-rework-ink">{urlProblem}</p> : null}
      <ProblemFixes pointer="/basics/repository/connection" />
      <ProblemFixes pointer="/basics/repository/token" />
      <p id={help} className="m-0 text-[13px] leading-snug text-muted">
        {repository?.url
          ? 'Cloned into the box before the agent starts. The token reaches the agent only when an environment names its secret.'
          : 'Without one, agents work without a checkout and open no pull requests.'}
        {token ? (
          <>
            {' '}
            <Button variant="ghost" className="h-auto p-0 max-md:min-h-11" onClick={() => edit({ token: '' })}>
              Use no token
            </Button>
          </>
        ) : null}
      </p>
    </div>
  );
}

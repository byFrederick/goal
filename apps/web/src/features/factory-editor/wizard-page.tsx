// The New factory wizard (D §10.1, §10.3, §10.11): /o/$org/new/$step in the sidebar, with no
// sidebar. Start chooses how to begin and creates the factory; from then on `?factory=` names the
// new factory's id and the steps edit its draft in the editor's section views. `start` with a
// factory resumes it (the Factories home's "Continue setup"): it opens the first step not done.
// Starting a factory needs factory:create (owners and admins); a draft factory the caller may not
// edit (no definition:edit) opens view only, as in Definition.
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { useEffect } from 'react';
import { ApiError } from '../../api/client';
import { draftModelQuery } from '../../api/queries';
import { ShellFrame } from '../../app/app-shell';
import { useFactoryById } from '../../shared/factory';
import { useOrg, useOrgHolds } from '../../shared/org';
import { holds, reasons } from '../../shared/permissions';
import { EmptyState } from '../../ui/empty-state';
import { DraftSessionProvider, useDraft, useModel } from './draft/use-draft';
import { StartStep } from './start/start-step';
import { WizardEditor } from './wizard-editor';
import { doneSteps, readProgress, resumeStep, type WizardStep, wizardStorage } from './wizard-progress';
import type { WizardSearch } from './wizard-search';

export function WizardPage({ step, search }: { step: WizardStep; search: WizardSearch }) {
  const org = useOrg();
  const { factory, from: _from, template: _template, ...section } = search;
  const mayCreate = useOrgHolds('factory:create');
  // A factory the list does not have yet was just created here: its creator edits it.
  const listed = useFactoryById(factory ?? '');
  const readOnly = listed !== undefined && !holds(listed, 'definition:edit');

  return (
    <ShellFrame>
      {factory ? (
        <DraftSessionProvider key={`${factory}:${readOnly}`} org={org.id} factory={factory} readOnly={readOnly}>
          {step === 'start' ? (
            <Resume factory={factory} />
          ) : (
            <WizardEditor step={step} factory={factory} search={section} />
          )}
        </DraftSessionProvider>
      ) : mayCreate ? (
        <StartStep search={search} />
      ) : (
        <main id="main" className="flex flex-col gap-4">
          <h1 className="m-0 text-[34px] leading-[1.05] font-normal tracking-display md:text-5xl">New factory</h1>
          <EmptyState title="Only owners and admins start factories">
            {reasons.newFactory(org.name)}{' '}
            <Link
              to="/o/$org"
              params={{ org: org.slug }}
              className="text-ink underline underline-offset-3 hover:text-run"
            >
              Back to the factories
            </Link>
          </EmptyState>
        </main>
      )}
    </ShellFrame>
  );
}

/** Opens a draft factory's first step not done, once its draft has loaded. */
function Resume({ factory }: { factory: string }) {
  const org = useOrg();
  const navigate = useNavigate();
  const ready = useDraft((s) => s.ready);
  const model = useModel();
  const draft = useQuery(draftModelQuery(org.id, factory));
  const failed = draft.error instanceof ApiError && draft.error.code !== 'no-draft' ? draft.error : null;

  useEffect(() => {
    if (!ready) return;
    const step = resumeStep(doneSteps(readProgress(wizardStorage(), factory), model));
    void navigate({ to: '/o/$org/new/$step', params: { org: org.slug, step }, search: { factory }, replace: true });
  }, [ready, model, factory, navigate, org.slug]);

  if (failed) {
    return (
      <div
        role="alert"
        className="flex max-w-xl flex-col gap-3 rounded-card border border-rework-tint bg-rework-tint p-5"
      >
        <h1 className="m-0 text-xl font-medium text-rework-ink">This draft factory can’t be opened</h1>
        <p className="m-0 text-[15px] leading-relaxed">{failed.detail ?? failed.title}</p>
        <Link to="/o/$org" params={{ org: org.slug }} className="text-ink underline underline-offset-3 hover:text-run">
          Back to the factories
        </Link>
      </div>
    );
  }
  return (
    <p role="status" aria-busy="true" className="m-0 text-sm text-muted">
      Opening the draft…
    </p>
  );
}

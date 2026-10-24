// The New factory wizard (D §10.1, §10.3, §10.11): /o/$org/new/$step on the rail, with no
// sidebar. Start chooses how to begin and creates the factory; from then on `?factory=` names the
// new factory's id and the steps edit its draft in the editor's section views. `start` with a
// factory resumes it (the Factories home's "Continue setup"): it opens the first step not done.
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { useEffect } from 'react';
import { ApiError } from '../../api/client';
import { draftModelQuery } from '../../api/queries';
import { ShellFrame } from '../../app/app-shell';
import { useOrg } from '../../shared/org';
import { DraftSessionProvider, useDraft, useModel } from './draft/use-draft';
import { StartStep } from './start/start-step';
import { WizardEditor } from './wizard-editor';
import { doneSteps, readProgress, resumeStep, type WizardStep, wizardStorage } from './wizard-progress';
import type { WizardSearch } from './wizard-search';

export function WizardPage({ step, search }: { step: WizardStep; search: WizardSearch }) {
  const org = useOrg();
  const { factory, from: _from, template: _template, ...section } = search;

  return (
    <ShellFrame sidebar={null}>
      {factory ? (
        <DraftSessionProvider key={factory} org={org.id} factory={factory}>
          {step === 'start' ? (
            <Resume factory={factory} />
          ) : (
            <WizardEditor step={step} factory={factory} search={section} />
          )}
        </DraftSessionProvider>
      ) : (
        <StartStep search={search} />
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
        className="flex max-w-xl flex-col gap-3 rounded-card border border-rework-line bg-rework-tint p-5"
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

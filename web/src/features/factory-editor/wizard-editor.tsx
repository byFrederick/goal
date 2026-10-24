// Steps 2–6 of the New factory wizard (D §10.3, canvas New-Basics … New-Review): the new
// factory's draft in the same section views as Definition (place "wizard"), framed by the
// wizard: its name with the Draft chip and where it came from, "Draft saved 14:32", Visual |
// YAML, Save and exit, the steps, and Back / Continue. Continue records the step as done. A
// factory never published can be deleted from here; one already published is edited in its
// Definition instead.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { ChevronRight } from 'lucide-react';
import { useState } from 'react';
import { ApiError } from '../../api/client';
import { deleteFactory } from '../../api/definitions';
import { draftQuery, factoriesQuery, factoryQuery, keys, templatesQuery } from '../../api/queries';
import { useOrg } from '../../shared/org';
import { reasons } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { Chip } from '../../ui/chip';
import { ConfirmDialog } from '../../ui/confirm-dialog';
import { EmptyState } from '../../ui/empty-state';
import { ViewOnlyBanner } from '../../ui/view-only-banner';
import { SectionView } from './definition-page';
import { useDraft, useDraftSession, useModel } from './draft/use-draft';
import { ConflictDialog, DiscardedDialog, FileConflictDialog } from './frame/conflict-dialog';
import { useUndoKeys } from './frame/editor-frame';
import { UnsavedGuard } from './frame/unsaved-guard';
import { DraftSaved, WizardFooter, WizardHeader, WizardViewToggle } from './frame/wizard-frame';
import { WizardSteps } from './frame/wizard-steps';
import { yamlView } from './section-views';
import type { DefinitionSearch } from './sections';
import { sourceSentence } from './start/start-choice';
import {
  doneSteps,
  nextStep,
  previousStep,
  progressKey,
  readProgress,
  recordDone,
  stepLabel,
  type WizardStep,
  wizardStorage,
} from './wizard-progress';

type EditorStep = Exclude<WizardStep, 'start'>;

export function WizardEditor({
  step,
  factory,
  search,
}: {
  step: EditorStep;
  factory: string;
  search: DefinitionSearch;
}) {
  const org = useOrg();
  const navigate = useNavigate();
  const client = useQueryClient();
  const session = useDraftSession();
  const s = useDraft((state) => state);
  const model = useModel();
  const detail = useQuery(factoryQuery(org.id, factory));
  const draft = useQuery(draftQuery(org.id, factory)).data;
  const templates = useQuery({ ...templatesQuery(org.id), enabled: draft?.source.kind === 'template' }).data;
  const factories = useQuery({ ...factoriesQuery(org.id), enabled: draft?.source.kind === 'clone' }).data;
  const [recorded, setRecorded] = useState(() => readProgress(wizardStorage(), factory));
  const [conflictOpen, setConflictOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  useUndoKeys(session);

  // A new conflict opens its dialog; closing it leaves "Resolve…" beside Draft saved.
  const [lastStatus, setLastStatus] = useState(s.status);
  if (s.status !== lastStatus) {
    setLastStatus(s.status);
    if (s.status === 'conflict') setConflictOpen(true);
  }

  const remove = useMutation({
    mutationFn: () => deleteFactory(org.id, factory),
    onSuccess: () => {
      try {
        wizardStorage()?.removeItem(progressKey(factory));
      } catch {}
      void client.invalidateQueries({ queryKey: keys.factories(org.id) });
      void client.invalidateQueries({ queryKey: keys.overviews(org.id) });
      void navigate({ to: '/o/$org', params: { org: org.slug }, ignoreBlocker: true });
    },
  });

  const name = model.basics.name || detail.data?.name || 'New factory';

  if (detail.data?.current) {
    return (
      <EmptyState title={`${detail.data.name} is published`}>
        Its setup is done: v{detail.data.current.number} runs. Change it in its{' '}
        <Link
          to="/o/$org/f/$factory/definition"
          params={{ org: org.slug, factory: detail.data.name }}
          className="text-ink underline underline-offset-3 hover:text-run"
        >
          Definition
        </Link>
        .
      </EmptyState>
    );
  }

  const source = draft?.source;
  const templateName = source?.template?.split('@')[0];
  const from = sourceSentence(source, {
    templateTitle: templates?.find((t) => t.name === templateName)?.title,
    factoryName: factories?.find((f) => f.id === source?.factory || f.name === source?.factory)?.name,
  });
  const done = doneSteps(recorded, model);
  const next = nextStep(step);
  const back = previousStep(step);
  const yaml = search.mode === 'yaml' && yamlView !== null;
  const searchOf = (mode?: 'yaml') => ({ factory, ...(mode ? { mode } : {}) });
  const go = (to: WizardStep, mode?: 'yaml') =>
    navigate({ to: '/o/$org/new/$step', params: { org: org.slug, step: to }, search: searchOf(mode) });

  const saveAndExit = async () => {
    await session.flush();
    await navigate({ to: '/o/$org', params: { org: org.slug } });
  };

  const continueTo = (to: WizardStep) => {
    setRecorded(recordDone(wizardStorage(), factory, step));
    void go(to);
  };

  const footerButton = 'md:h-10 md:text-sm';
  const viewOnly = session.readOnly;
  const deleteError =
    remove.error instanceof ApiError && remove.error.code === 'factory-published'
      ? 'It was published in the meantime, so it stays. Change it in its Definition.'
      : remove.error
        ? `It wasn’t deleted: ${remove.error instanceof ApiError ? (remove.error.detail ?? remove.error.title) : 'goald can’t be reached'}.`
        : undefined;

  return (
    <>
      <WizardHeader
        title={name}
        chip={<Chip className="border-ink">Draft</Chip>}
        description={`${from ? `${from} ` : ''}Everything here stays editable later in the factory’s Definition.`}
        actions={
          <>
            {viewOnly ? null : (
              <DraftSaved
                session={{ ...s, savedAt: s.savedAt ?? draft?.updated_at ?? null }}
                onRetry={() => session.retryNow()}
                onResolve={() => setConflictOpen(true)}
              />
            )}
            <WizardViewToggle
              yaml={yaml}
              yamlUnavailable={yamlView ? undefined : 'arrives later'}
              onChange={(y) => void go(step, y ? 'yaml' : undefined)}
            />
            {viewOnly ? null : (
              <Button variant="white" className="h-11 md:h-10" onClick={() => void saveAndExit()}>
                Save and exit
              </Button>
            )}
          </>
        }
      />
      {viewOnly ? <ViewOnlyBanner>{reasons.definition(name)}</ViewOnlyBanner> : null}
      <WizardSteps current={step} done={done} factory={factory} />

      <SectionView place="wizard" section={step} search={search} />

      <WizardFooter
        step={step}
        start={
          <>
            {back && back !== 'start' ? (
              <Button asChild variant="white" size="lg" className={footerButton}>
                <Link to="/o/$org/new/$step" params={{ org: org.slug, step: back }} search={searchOf()}>
                  Back
                </Link>
              </Button>
            ) : null}
            {viewOnly ? null : (
              <Button
                variant="ghost"
                size="lg"
                className={`${footerButton} text-rework-ink`}
                aria-haspopup="dialog"
                onClick={() => setDeleteOpen(true)}
              >
                Delete this draft factory…
              </Button>
            )}
          </>
        }
        end={
          next ? (
            <Button variant="primary" size="lg" className={footerButton} onClick={() => continueTo(next)}>
              Continue to {stepLabel(next)}
              <ChevronRight size={14} strokeWidth={1.75} aria-hidden="true" />
            </Button>
          ) : null
        }
      />

      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={(open) => {
          setDeleteOpen(open);
          if (!open) remove.reset();
        }}
        title={`Delete ${name}?`}
        description="It was never published, so nothing runs from it. Its draft and files are deleted for good."
        confirmLabel="Delete draft factory"
        pendingLabel="Deleting…"
        tone="danger"
        pending={remove.isPending}
        onConfirm={() => remove.mutate()}
      >
        {deleteError ? (
          <p role="alert" className="m-0 rounded-control border border-rework-line bg-rework-tint px-3 py-2.5 text-sm">
            {deleteError}
          </p>
        ) : null}
      </ConfirmDialog>
      <ConflictDialog open={conflictOpen && Object.keys(s.fileConflicts).length === 0} onOpenChange={setConflictOpen} />
      <FileConflictDialog
        open={conflictOpen && Object.keys(s.fileConflicts).length > 0}
        onOpenChange={setConflictOpen}
      />
      <DiscardedDialog
        onLeave={() => void navigate({ to: '/o/$org', params: { org: org.slug }, ignoreBlocker: true })}
      />
      <UnsavedGuard />
    </>
  );
}

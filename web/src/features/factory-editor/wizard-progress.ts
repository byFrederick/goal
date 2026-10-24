// Which steps of the New factory wizard are done (D §10.3): a step is done once it was left with
// Continue, remembered per factory in localStorage["goal.wizard.<factory id>"], a per-viewer
// convenience. Without that record (another browser, cleared storage) the steps with content
// count as done. Resuming a draft factory opens the first step not done.
import type { Model } from '../../shared/definition/model';
import { sectionInfo } from './sections';

/** The wizard's steps: Start, then the editor's five sections. */
export const wizardSteps = ['start', 'basics', 'line', 'agents', 'connections', 'review'] as const;

export type WizardStep = (typeof wizardSteps)[number];

export function isWizardStep(value: string): value is WizardStep {
  return (wizardSteps as readonly string[]).includes(value);
}

/** A step's name: Start, then the sections' ("Review and publish"). */
export const stepLabel = (step: WizardStep) => (step === 'start' ? 'Start' : sectionInfo[step].label);

/** The step after `step`; null after Review. */
export function nextStep(step: WizardStep): WizardStep | null {
  return wizardSteps[wizardSteps.indexOf(step) + 1] ?? null;
}

/** The step before `step`; null before Start. */
export function previousStep(step: WizardStep): WizardStep | null {
  return wizardSteps[wizardSteps.indexOf(step) - 1] ?? null;
}

export const progressKey = (factory: string) => `goal.wizard.${factory}`;

/** This browser's localStorage, or undefined where reading it throws (blocked site data). */
export function wizardStorage(): Storage | undefined {
  try {
    return window.localStorage;
  } catch {
    return undefined;
  }
}

/** The steps recorded as done, or null when nothing is recorded (or storage can't be read). */
export function readProgress(storage: Pick<Storage, 'getItem'> | undefined, factory: string): WizardStep[] | null {
  try {
    const raw = storage?.getItem(progressKey(factory));
    if (!raw) return null;
    const parsed: unknown = JSON.parse(raw);
    return Array.isArray(parsed)
      ? parsed.filter((s): s is WizardStep => typeof s === 'string' && isWizardStep(s))
      : null;
  } catch {
    return null;
  }
}

/** Records `step` as done; storage that refuses (private mode, quota) only loses the convenience. */
export function recordDone(
  storage: Pick<Storage, 'getItem' | 'setItem'> | undefined,
  factory: string,
  step: WizardStep,
): WizardStep[] {
  const done = new Set(readProgress(storage, factory) ?? ['start']);
  done.add(step);
  const out = wizardSteps.filter((s) => done.has(s));
  try {
    storage?.setItem(progressKey(factory), JSON.stringify(out));
  } catch {}
  return out;
}

/** Whether a section of the draft has content of its own: what counts as done without a record. */
function hasContent(step: WizardStep, model: Model): boolean {
  switch (step) {
    case 'start':
      return true;
    case 'basics':
      return Boolean(model.basics.title || model.basics.purpose || (model.basics.work_types ?? []).length > 0);
    case 'line':
      return (model.line.stations ?? []).length > 0;
    case 'agents':
      return (model.agents.agents ?? []).length > 0;
    case 'connections':
      return model.connections.length > 0;
    case 'review':
      return false;
  }
}

/** The done steps: the record when there is one (Start is done once the factory exists), else those with content. */
export function doneSteps(recorded: readonly WizardStep[] | null, model: Model): ReadonlySet<WizardStep> {
  if (recorded) return new Set<WizardStep>(['start', ...recorded]);
  return new Set(wizardSteps.filter((s) => hasContent(s, model)));
}

/** Where resuming opens: the first step not done, Review when every other one is. */
export function resumeStep(done: ReadonlySet<WizardStep>): Exclude<WizardStep, 'start'> {
  const open = wizardSteps.find((s) => s !== 'start' && !done.has(s));
  return open && open !== 'start' ? open : 'review';
}

// The New factory wizard's route skeleton (D §10.1): /o/$org/new/$step on the rail, with no
// sidebar. D13 adds the Start step, the step nav, "Draft saved" and Save and exit; until then the
// rail's New factory stays "Later" and nothing links here. From step 2 on, `?factory=` names the
// new factory's id and the step renders the same section view as Definition (place "wizard").
import { Link } from '@tanstack/react-router';
import { ShellFrame } from '../../app/app-shell';
import { useOrg } from '../../shared/org';
import { EmptyState } from '../../ui/empty-state';
import { SectionView } from './definition-page';
import { DraftSessionProvider } from './draft/use-draft';
import { type DefinitionSearch, validateDefinitionSearch } from './sections';

/** The wizard's steps: Start, then the editor's five sections. */
export const wizardSteps = ['start', 'basics', 'line', 'agents', 'connections', 'review'] as const;

export type WizardStep = (typeof wizardSteps)[number];

export function isWizardStep(value: string): value is WizardStep {
  return (wizardSteps as readonly string[]).includes(value);
}

export interface WizardSearch extends DefinitionSearch {
  /** The new factory's id, from step 2 on. */
  factory?: string;
  /** How to start, from the Factories home's cards. */
  from?: 'blank' | 'template' | 'clone' | 'yaml';
  template?: string;
}

export function validateWizardSearch(search: Record<string, unknown>): WizardSearch {
  const out: WizardSearch = validateDefinitionSearch(search);
  if (typeof search.factory === 'string' && search.factory !== '') out.factory = search.factory;
  if (search.from === 'blank' || search.from === 'template' || search.from === 'clone' || search.from === 'yaml') {
    out.from = search.from;
  }
  if (typeof search.template === 'string' && search.template !== '') out.template = search.template;
  return out;
}

export function WizardPage({ step, search }: { step: WizardStep; search: WizardSearch }) {
  const org = useOrg();

  return (
    <ShellFrame sidebar={null}>
      <header className="flex flex-col gap-2.5">
        <nav aria-label="Breadcrumb" className="text-sm text-muted">
          <ol className="m-0 flex list-none gap-1.5 p-0">
            <li>
              <Link to="/o/$org" params={{ org: org.slug }} className="text-muted hover:text-run">
                Factories
              </Link>
            </li>
            <li aria-hidden="true">/</li>
            <li aria-current="page">New factory</li>
          </ol>
        </nav>
        <h1 className="m-0 text-[34px] leading-[1.05] font-normal tracking-display md:text-[44px]">New factory</h1>
      </header>
      {step !== 'start' && search.factory ? (
        <DraftSessionProvider key={search.factory} org={org.id} factory={search.factory}>
          <SectionView place="wizard" section={step} search={search} />
        </DraftSessionProvider>
      ) : (
        <EmptyState title="Start a factory">
          Starting a factory in the browser arrives later. Until then, push a definition from a working copy with{' '}
          <code className="font-mono text-[13px] text-ink">goal factory push &lt;dir&gt;</code>.
        </EmptyState>
      )}
    </ShellFrame>
  );
}

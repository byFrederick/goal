// Step 5, "Set budgets and targets" (docs/design/screens/get-started/budget.jpg, ADR 0032 decision 4). The
// factory from step 3, its draft open: the budget fields and the flow targets (budget-fields.tsx) save to the
// draft as they change. Continue writes the picture's defaults for any field still unset, flushes the draft,
// publishes it as the factory's first version and starts the line, then goes to step 6. A publish that goald
// refuses shows its reason on the step, and the step stays.
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { useState } from 'react';
import { ApiError } from '../../api/client';
import { publishDraft } from '../../api/definitions';
import { defKeys, factoryQuery, keys } from '../../api/queries';
import type { DefinitionProblem, OrgSummary } from '../../api/types';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { DraftSessionProvider, useDraft, useDraftSession, useSection } from '../factory-editor/draft/use-draft';
import { stationLabel } from '../factory-editor/line/labels';
import { defaultEdits } from './budget-draft';
import { BudgetFields, useBudgetValues } from './budget-fields';
import { BudgetCard } from './budget-gauge';
import type { OnboardingSearch } from './code-search';
import { codeCard, FactoryCard, organizationCard, previewSlots } from './factory-preview';
import { OnboardingLayout, PreviewPlaceholder } from './onboarding-layout';

export function BudgetStep({ org, search }: { org: OrgSummary; search: OnboardingSearch }) {
  const factoryId = search.factory;
  const factory = useQuery({ ...factoryQuery(org.id, factoryId ?? ''), enabled: factoryId !== undefined });

  if (!factoryId) {
    return <NoFactory org={org} search={search} message="Choose a template first, then set its budgets." />;
  }
  if (factory.isError) {
    return <NoFactory org={org} search={search} message="This factory can’t be read. Choose a template again." />;
  }
  if (factory.isPending) return <Loading org={org} search={search} factory={factoryId} />;
  return (
    <DraftSessionProvider org={org.id} factory={factoryId}>
      <DraftBudget org={org} search={search} factory={factoryId} />
    </DraftSessionProvider>
  );
}

/** Back to step 4 while the factory exists; back to step 3 (which makes one) when it does not. */
function Back({ org, search, factory }: { org: OrgSummary; search: OnboardingSearch; factory?: string }) {
  return (
    <Button asChild size="lg" variant="secondary">
      {factory ? (
        <Link to="/o/$org/get-started/$step" params={{ org: org.slug, step: 'line' }} search={{ factory }}>
          Back
        </Link>
      ) : (
        <Link
          to="/o/$org/get-started/$step"
          params={{ org: org.slug, step: 'factory' }}
          search={{ connection: search.connection, owner: search.owner, repos: search.repos }}
        >
          Back
        </Link>
      )}
    </Button>
  );
}

/** The step without a factory, or for a factory that cannot be read: a link back to step 3. */
function NoFactory({ org, search, message }: { org: OrgSummary; search: OnboardingSearch; message: string }) {
  return (
    <OnboardingLayout step="budget" org={org} back={<Back org={org} search={search} />}>
      <div className="flex flex-col gap-2">
        <h1 className="m-0 font-numeral text-kpi tracking-display">Set budgets and targets</h1>
      </div>
      <Callout tone="rework">{message}</Callout>
    </OnboardingLayout>
  );
}

function Loading({ org, search, factory }: { org: OrgSummary; search: OnboardingSearch; factory: string }) {
  return (
    <OnboardingLayout step="budget" org={org} back={<Back org={org} search={search} factory={factory} />}>
      <div aria-busy="true">
        <span role="status" className="sr-only">
          Loading
        </span>
      </div>
    </OnboardingLayout>
  );
}

/** The draft is read once it is open: until then the step shows its loading state. */
function DraftBudget({ org, search, factory }: { org: OrgSummary; search: OnboardingSearch; factory: string }) {
  const ready = useDraft((s) => s.ready);
  const hasDraft = useDraft((s) => s.hasDraft);
  if (!ready) return <Loading org={org} search={search} factory={factory} />;
  if (!hasDraft) {
    return (
      <NoFactory
        org={org}
        search={search}
        message="This factory has no draft to set budgets on. Choose a template again."
      />
    );
  }
  return <Budget org={org} search={search} factory={factory} />;
}

/** A refused publish: its message, and the problems goald listed for it. */
interface Refusal {
  message: string;
  problems: string[];
}

function refusalOf(err: unknown): Refusal {
  if (!(err instanceof ApiError)) {
    return { message: 'goald can’t be reached. Your draft stays saved; try again.', problems: [] };
  }
  if (err.code === 'definition-invalid') {
    const listed = (err.problem as { problems?: DefinitionProblem[] }).problems ?? [];
    return {
      message: 'The factory has problems, so it is not published. Fix them, then continue.',
      problems: listed.filter((p) => p.severity === 'error').map((p) => p.message),
    };
  }
  if (err.code === 'draft-base-moved') {
    return { message: 'Another version was published while you were here. Reload the step to see it.', problems: [] };
  }
  if (err.status === 412) {
    return { message: 'The draft changed while you were here. Continue again.', problems: [] };
  }
  return { message: `Not published: ${(err.detail ?? err.title).replace(/\.$/, '')}.`, problems: [] };
}

function Budget({ org, search, factory }: { org: OrgSummary; search: OnboardingSearch; factory: string }) {
  const session = useDraftSession();
  const client = useQueryClient();
  const navigate = useNavigate();
  const values = useBudgetValues();
  const { review, line, day, week } = values;
  const [pending, setPending] = useState(false);
  const [refusal, setRefusal] = useState<Refusal | null>(null);
  const factoryDetail = useQuery(factoryQuery(org.id, factory)).data;
  const basics = useSection('basics');
  const host = window.location.host;

  // Continue: fill the defaults in, save the draft, then publish it as the first version and start the line.
  const publish = async () => {
    if (pending) return;
    setPending(true);
    setRefusal(null);
    try {
      await session.flush();
      const defaults = defaultEdits(review, line);
      if (defaults.length > 0) session.edit(defaults);
      await session.flush();
      if (session.hasUnsaved()) {
        setRefusal({
          message: 'Your last changes aren’t saved yet. Continue once the status line says Autosaved.',
          problems: [],
        });
        return;
      }
      await publishDraft(session.org, factory, {
        revision: session.getSnapshot().revision,
        expected_current_version_id: null,
        start: true,
      });
      session.closeDraft();
      await Promise.all(
        [
          defKeys.draft(session.org, factory),
          defKeys.versions(session.org, factory),
          defKeys.versionsOf(session.org, factory),
          defKeys.factory(session.org, factory),
          defKeys.factories(session.org),
          defKeys.compares(session.org, factory),
          keys.board(session.org, factory),
        ].map((queryKey) => client.invalidateQueries({ queryKey })),
      );
      void navigate({
        to: '/o/$org/get-started/$step',
        params: { org: org.slug, step: 'intake' },
        search: { factory },
      });
    } catch (err) {
      setRefusal(refusalOf(err));
      if (err instanceof ApiError && err.status === 412) {
        void client.invalidateQueries({ queryKey: defKeys.draft(session.org, factory) });
      }
    } finally {
      setPending(false);
    }
  };

  const panelStations = (line.stations ?? []).map((s) => ({
    id: s.id,
    label: stationLabel(s),
    kind: s.kind,
    wip: s.wip,
  }));

  return (
    <OnboardingLayout
      step="budget"
      org={org}
      back={<Back org={org} search={search} factory={factory} />}
      action={
        <Button
          type="button"
          size="lg"
          variant="primary"
          disabled={pending}
          aria-busy={pending || undefined}
          onClick={() => void publish()}
        >
          Continue
        </Button>
      }
      preview={{
        ...previewSlots({
          organization: organizationCard(org, host),
          code: codeCard(search.repos?.[0]),
          factory: (
            <FactoryCard
              name={basics.name || factoryDetail?.name || ''}
              purpose={basics.purpose ?? ''}
              stations={panelStations}
            />
          ),
        }),
        cards: (
          <>
            <BudgetCard day={day} week={week} />
            <PreviewPlaceholder className="min-h-37.5" />
            <PreviewPlaceholder className="min-h-37.5" />
          </>
        ),
      }}
    >
      <div className="flex flex-col gap-2">
        <h1 className="m-0 font-numeral text-kpi tracking-display">Set budgets and targets</h1>
        <p className="m-0 text-heading font-normal text-ink-soft">
          Goal stops starting new work when a budget is spent, so a busy day never surprises you.
        </p>
      </div>

      <BudgetFields />

      {refusal ? (
        <Callout tone="rework">
          <span>{refusal.message}</span>
          {refusal.problems.length > 0 ? (
            <ul className="m-0 mt-2 flex list-disc flex-col gap-1 pl-5">
              {refusal.problems.map((problem) => (
                <li key={problem}>{problem}</li>
              ))}
            </ul>
          ) : null}
        </Callout>
      ) : null}
    </OnboardingLayout>
  );
}

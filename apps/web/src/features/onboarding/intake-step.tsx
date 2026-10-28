// Step 6, "Choose where work comes from" (docs/design/screens/get-started/triggers.jpg, ADR 0032 decisions 2
// and 5). The factory from step 3, published by step 5: its starter triggers are the template's, off until a
// person turns them on. A switch starts at the trigger's `enabled`; a trigger that is already on stays on.
// "Turn on N triggers" writes every switched trigger that was off, in one draft and one publish
// (setTriggersEnabled), then goes to step 7. A trigger whose source needs an endpoint the factory lacks
// (problem.kind missing_connection) shows Connect and its switch stays off. Its four states: loading, error
// (with Retry), empty (no starter triggers) and ready.
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { useState } from 'react';
import { ApiError } from '../../api/client';
import { defKeys, factoryQuery, intakeKeys, intakeQuery, versionModelQuery } from '../../api/queries';
import type { Factory, Intake, IntakeTrigger, OrgSummary, VersionModel } from '../../api/types';
import { modelOf } from '../../shared/definition/model';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { Switch } from '../../ui/switch';
import { stationLabel } from '../factory-editor/line/labels';
import { OpenDraftError, setTriggersEnabled } from '../intake/set-trigger-enabled';
import { sourceIcon } from '../intake/triggers-table';
import { BudgetCard } from './budget-gauge';
import { budgetRanges } from './budget-hints';
import type { OnboardingSearch } from './code-search';
import { codeCard, FactoryCard, organizationCard, previewSlots } from './factory-preview';
import { OnboardingLayout, PreviewPlaceholder } from './onboarding-layout';

export function IntakeStep({ org, search }: { org: OrgSummary; search: OnboardingSearch }) {
  const factoryId = search.factory;
  const factory = useQuery({ ...factoryQuery(org.id, factoryId ?? ''), enabled: factoryId !== undefined });

  if (!factoryId) {
    return <NoFactory org={org} search={search} message="Choose a template first, then pick where work comes from." />;
  }
  if (factory.isError) {
    return <NoFactory org={org} search={search} message="This factory can’t be read. Choose a template again." />;
  }
  if (factory.isPending) return <Loading org={org} search={search} factory={factoryId} />;
  return <Triggers org={org} search={search} factoryId={factoryId} factory={factory.data} />;
}

/** Back to step 5 while the factory exists; back to step 3 (which makes one) when it does not. */
function Back({ org, search, factory }: { org: OrgSummary; search: OnboardingSearch; factory?: string }) {
  return (
    <Button asChild size="lg" variant="secondary">
      {factory ? (
        <Link to="/o/$org/get-started/$step" params={{ org: org.slug, step: 'budget' }} search={{ factory }}>
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
    <OnboardingLayout step="intake" org={org} back={<Back org={org} search={search} />}>
      <div className="flex flex-col gap-2">
        <h1 className="m-0 font-numeral text-kpi tracking-display">Choose where work comes from</h1>
      </div>
      <Callout tone="rework">{message}</Callout>
    </OnboardingLayout>
  );
}

function Loading({ org, search, factory }: { org: OrgSummary; search: OnboardingSearch; factory: string }) {
  return (
    <OnboardingLayout step="intake" org={org} back={<Back org={org} search={search} factory={factory} />}>
      <div aria-busy="true">
        <span role="status" className="sr-only">
          Loading
        </span>
      </div>
    </OnboardingLayout>
  );
}

/** Reads the triggers and the published model; the step shows loading until both are there. */
function Triggers({
  org,
  search,
  factoryId,
  factory,
}: {
  org: OrgSummary;
  search: OnboardingSearch;
  factoryId: string;
  factory: Factory;
}) {
  const intake = useQuery(intakeQuery(org.id, factoryId));
  const model = useQuery(versionModelQuery(org.id, factoryId, 'current'));

  if (intake.isPending || model.isPending) return <Loading org={org} search={search} factory={factoryId} />;
  if (intake.isError || model.isError) {
    const retry = () => {
      void intake.refetch();
      void model.refetch();
    };
    return (
      <OnboardingLayout step="intake" org={org} back={<Back org={org} search={search} factory={factoryId} />}>
        <div className="flex flex-col gap-2">
          <h1 className="m-0 font-numeral text-kpi tracking-display">Choose where work comes from</h1>
        </div>
        <Callout tone="rework">
          <span>The factory’s triggers can’t be read.</span>
          <Button type="button" size="sm" variant="secondary" className="mt-2" onClick={retry}>
            Retry
          </Button>
        </Callout>
      </OnboardingLayout>
    );
  }
  return (
    <Ready
      org={org}
      search={search}
      factoryId={factoryId}
      factory={factory}
      intake={intake.data}
      model={model.data.model}
    />
  );
}

/** A trigger the factory cannot run yet: it needs an endpoint the factory does not have. */
function needsConnection(trigger: IntakeTrigger): boolean {
  return trigger.problem?.kind === 'missing_connection';
}

/** The sentence for what a trigger creates: "Creates a feature at Spec", "in the expedite lane" when it is expedited. */
function createsSentence(trigger: IntakeTrigger): string {
  const { type, station, expedite } = trigger.create;
  const article = /^[aeiou]/i.test(type) ? 'an' : 'a';
  return `Creates ${article} ${type} at ${station}${expedite ? ' in the expedite lane' : ''}`;
}

/** A refused write, as the step says it. */
function refusalOf(err: unknown): string {
  if (err instanceof OpenDraftError) {
    return 'The factory has an open draft, so nothing was turned on. Publish or discard it in Definition, then try again.';
  }
  if (err instanceof ApiError) {
    if (err.status === 412 || err.code === 'draft-base-moved') {
      return 'Another version was published while you were here. Reload the step to see it.';
    }
    return `Not turned on: ${(err.detail ?? err.title).replace(/\.$/, '')}.`;
  }
  return 'goald can’t be reached. Nothing was turned on; try again.';
}

function Ready({
  org,
  search,
  factoryId,
  factory,
  intake,
  model,
}: {
  org: OrgSummary;
  search: OnboardingSearch;
  factoryId: string;
  factory: Factory;
  intake: Intake;
  model: VersionModel['model'];
}) {
  const client = useQueryClient();
  const navigate = useNavigate();
  // The switches the person has turned on in this visit. A trigger that is already on is on by its `enabled`.
  const [chosen, setChosen] = useState<ReadonlySet<string>>(() => new Set());
  const [pending, setPending] = useState(false);
  const [refusal, setRefusal] = useState<string | null>(null);

  const triggers = intake.triggers;
  const isOn = (t: IntakeTrigger) => t.enabled || chosen.has(t.name);
  const toTurnOn = triggers.filter((t) => !t.enabled && chosen.has(t.name) && !needsConnection(t));
  const count = toTurnOn.length;
  const label = count === 0 ? 'Continue' : count === 1 ? 'Turn on 1 trigger' : `Turn on ${count} triggers`;

  const goToPeople = () => {
    void navigate({
      to: '/o/$org/get-started/$step',
      params: { org: org.slug, step: 'people' },
      search: { factory: factoryId },
    });
  };

  const turnOn = async () => {
    if (pending) return;
    if (count === 0) {
      goToPeople();
      return;
    }
    setPending(true);
    setRefusal(null);
    try {
      await setTriggersEnabled({
        org: org.id,
        factory: factoryId,
        currentVersionId: factory.current?.id ?? null,
        triggers: toTurnOn.map((t) => t.name),
        enabled: true,
      });
      await Promise.all(
        [
          intakeKeys.root(org.id, factoryId),
          defKeys.draft(org.id, factoryId),
          defKeys.versions(org.id, factoryId),
          defKeys.factory(org.id, factoryId),
          defKeys.factories(org.id),
          defKeys.compares(org.id, factoryId),
        ].map((queryKey) => client.invalidateQueries({ queryKey })),
      );
      goToPeople();
    } catch (err) {
      setRefusal(refusalOf(err));
      if (err instanceof ApiError && err.status === 412) {
        void client.invalidateQueries({ queryKey: intakeKeys.root(org.id, factoryId) });
      }
    } finally {
      setPending(false);
    }
  };

  const { line, review, basics } = modelOf(model);
  const budget = review.budget;
  const day = budget?.daily_usd ?? budgetRanges.day.fallback;
  const week = budget?.weekly_usd ?? budgetRanges.week.fallback;
  const panelStations = (line.stations ?? []).map((s) => ({
    id: s.id,
    label: stationLabel(s),
    kind: s.kind,
    wip: s.wip,
  }));
  const switchedOn = triggers.filter(isOn);

  return (
    <OnboardingLayout
      step="intake"
      org={org}
      back={<Back org={org} search={search} factory={factoryId} />}
      action={
        <Button
          type="button"
          size="lg"
          variant="primary"
          disabled={pending}
          aria-busy={pending || undefined}
          onClick={() => void turnOn()}
        >
          {label}
        </Button>
      }
      preview={{
        ...previewSlots({
          organization: organizationCard(org, window.location.host),
          code: codeCard(search.repos?.[0]),
          factory: <FactoryCard name={factory.name} purpose={basics.purpose ?? ''} stations={panelStations} />,
        }),
        cards: (
          <>
            <BudgetCard day={day} week={week} />
            <IntakeCard names={switchedOn.map((t) => t.name)} />
            <PreviewPlaceholder className="min-h-37.5" />
          </>
        ),
      }}
    >
      <div className="flex flex-col gap-2">
        <h1 className="m-0 font-numeral text-kpi tracking-display">Choose where work comes from</h1>
        <p className="m-0 text-heading font-normal text-ink-soft">
          Triggers turn events and schedules into items on the line. You can change them any time in Intake.
        </p>
      </div>

      {triggers.length === 0 ? (
        <p className="m-0 text-body text-ink-soft">
          This factory has no starter triggers. You can add triggers in Intake later.
        </p>
      ) : (
        <>
          <section aria-labelledby="starter-triggers" className="flex flex-col rounded-card border border-line">
            <h2 id="starter-triggers" className="m-0 p-4 text-heading">
              Starter triggers
            </h2>
            {triggers.map((trigger) => (
              <TriggerRow
                key={trigger.name}
                org={org}
                factory={factory}
                trigger={trigger}
                on={isOn(trigger)}
                disabled={trigger.enabled || needsConnection(trigger) || pending}
                onChange={(next) =>
                  setChosen((prev) => {
                    const out = new Set(prev);
                    if (next) out.add(trigger.name);
                    else out.delete(trigger.name);
                    return out;
                  })
                }
              />
            ))}
          </section>
          <p className="m-0 flex flex-wrap items-center gap-2 text-secondary">
            <span className="text-ink-soft">Always on:</span>
            <Chip>New item</Chip>
            <Chip>goal item create</Chip>
            <Chip>The API</Chip>
          </p>
        </>
      )}

      {refusal ? <Callout tone="rework">{refusal}</Callout> : null}
    </OnboardingLayout>
  );
}

/** One starter trigger: its source's tile, its name, when it fires, what it creates, and its switch or Connect. */
function TriggerRow({
  org,
  factory,
  trigger,
  on,
  disabled,
  onChange,
}: {
  org: OrgSummary;
  factory: Factory;
  trigger: IntakeTrigger;
  on: boolean;
  disabled: boolean;
  onChange: (next: boolean) => void;
}) {
  const { icon: Icon, tile, label } = sourceIcon[trigger.source];
  const missing = needsConnection(trigger);
  return (
    <div className="flex items-center gap-3 border-t border-line p-4">
      <span
        aria-hidden="true"
        className={cn('flex size-8.5 flex-none items-center justify-center rounded-cell text-white', tile)}
      >
        <Icon className="size-4" />
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-px">
        <span className="font-control text-body text-ink">{trigger.name}</span>
        <span className="text-secondary break-words text-ink-soft">{trigger.when}</span>
        <span className="text-secondary break-words text-muted">{createsSentence(trigger)}</span>
        {trigger.problem && !missing ? (
          <span className="text-meta font-semibold text-rework-ink">{trigger.problem.message}</span>
        ) : null}
      </div>
      <div className="flex flex-none items-center gap-3">
        {missing ? (
          <Button asChild size="sm" variant="secondary">
            <Link to="/o/$org/f/$factory/intake/webhooks" params={{ org: org.slug, factory: factory.name }}>
              Connect {label}
            </Link>
          </Button>
        ) : null}
        <Switch aria-label={trigger.name} checked={on} disabled={disabled} onCheckedChange={onChange} />
      </div>
    </div>
  );
}

/** The panel's Intake card: the triggers switched on, then what is always on. */
function IntakeCard({ names }: { names: string[] }) {
  return (
    <section
      aria-label="Intake"
      className="flex min-w-0 flex-col gap-2 rounded-card border border-run-soft bg-andon-raised p-4 ring-4 ring-run-soft/25"
    >
      <span className="text-meta text-andon-text">Intake</span>
      {names.length > 0 ? (
        <ul className="m-0 flex list-none flex-col gap-1 p-0">
          {names.map((name) => (
            <li key={name} className="text-secondary break-words text-white">
              {name}
            </li>
          ))}
        </ul>
      ) : null}
      <span className="text-secondary break-words text-andon-text">And New item, CLI, API</span>
    </section>
  );
}

// The dark panel of steps 7 and 8 (docs/design/screens/get-started/invite.jpg and first-item.jpg): the
// organization, code and factory cards as step 6 leaves them, then the budget, intake and people cards, and on
// step 8 the running item under them. The factory's item badge marks the station the item is at.
import type { Factory, FactoryModel, Intake, OrgSummary } from '../../api/types';
import { modelOf } from '../../shared/definition/model';
import { stationLabel } from '../factory-editor/line/labels';
import { BudgetCard } from './budget-gauge';
import { budgetRanges } from './budget-hints';
import type { OnboardingSearch } from './code-search';
import { codeCard, FactoryCard, organizationCard, previewSlots, RunningItemCard } from './factory-preview';
import { IntakeCard } from './intake-step';
import type { PreviewSlots } from './onboarding-layout';
import { PeopleCard } from './people-card';

/** The item the panel shows: where it is, how long it has been running and what it has cost. */
export interface PanelItem {
  number: number;
  title: string;
  /** The station's id, as the item has it. */
  station: string;
  minutes: number;
  cost: number;
}

export function stepPanel({
  org,
  search,
  factory,
  model,
  intake,
  item,
}: {
  org: OrgSummary;
  search: OnboardingSearch;
  factory: Factory;
  model: FactoryModel;
  intake: Intake;
  item?: PanelItem | undefined;
}): PreviewSlots {
  const { line, review, basics } = modelOf(model);
  const budget = review.budget;
  const day = budget?.daily_usd ?? budgetRanges.day.fallback;
  const week = budget?.weekly_usd ?? budgetRanges.week.fallback;
  const stations = line.stations ?? [];
  const panelStations = stations.map((s) => ({ id: s.id, label: stationLabel(s), kind: s.kind, wip: s.wip }));
  const names = intake.triggers.filter((t) => t.enabled).map((t) => t.name);
  const at = item ? stations.find((s) => s.id === item.station) : undefined;

  return {
    ...previewSlots({
      organization: organizationCard(org, window.location.host),
      code: codeCard(search.repos?.[0]),
      factory: (
        <FactoryCard
          name={factory.name}
          purpose={basics.purpose ?? ''}
          stations={panelStations}
          item={item ? { station: item.station, number: item.number } : undefined}
        />
      ),
    }),
    cards: (
      <>
        <BudgetCard day={day} week={week} />
        <IntakeCard names={names} />
        <PeopleCard org={org} />
      </>
    ),
    item:
      item === undefined ? undefined : (
        <RunningItemCard
          number={item.number}
          title={item.title}
          station={at ? stationLabel(at) : item.station}
          minutes={item.minutes}
          cost={item.cost}
        />
      ),
  };
}

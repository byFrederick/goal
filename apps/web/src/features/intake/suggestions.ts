// The suggested triggers the Intake page offers: a fixed catalog of starting points, each a trigger the
// picker would create. Its Add goes to the trigger page with the choice in the address, as the picker's
// Continue does. A suggestion whose event the schema does not accept is listed disabled.
import { choosableEvents, newTriggerHref, type SourceKey, sourceByKey } from './sources';

export interface Suggestion {
  id: string;
  /** The tab it is listed under: a key of `suggestionGroups`. */
  group: SuggestionGroup;
  title: string;
  /** One line: what it does and what it creates. */
  body: string;
  /** The station it creates the item at. */
  station: string;
  source: SourceKey;
  /** The trigger's event key (the `on` of its address). */
  on: string;
}

export const suggestionGroups = ['software', 'incidents', 'code-health', 'pull-requests', 'security'] as const;
export type SuggestionGroup = (typeof suggestionGroups)[number];

/** The tab labels; the first is the factory's own, named by the page. */
export const suggestionLabels: Record<SuggestionGroup, string> = {
  software: 'For {factory}',
  incidents: 'Incidents',
  'code-health': 'Code health',
  'pull-requests': 'Pull requests',
  security: 'Security',
};

export const suggestions: readonly Suggestion[] = [
  {
    id: 'fix-failing-checks',
    group: 'software',
    title: 'Fix failing checks on pull requests',
    body: 'When a check fails on a factory pull request, the engineer that opened it gets a fix item.',
    station: 'Build',
    source: 'github',
    on: 'github.check_run.failed',
  },
  {
    id: 'answer-review-comments',
    group: 'software',
    title: 'Answer review comments by people',
    body: "A person's comment on a factory pull request sends the item back to Build with the comment as a note.",
    station: 'Build',
    source: 'github',
    on: 'github.pull_request.commented',
  },
  {
    id: 'weekly-vulnerability-sweep',
    group: 'software',
    title: 'Weekly vulnerability sweep',
    body: 'Every week, the architect looks for exploitable issues and files one fix item for each real finding.',
    station: 'Spec',
    source: 'schedule',
    on: 'schedule',
  },
];

/** Whether the picker can create the suggestion's trigger: its event is one the schema accepts. */
export function isChoosable(suggestion: Suggestion): boolean {
  return choosableEvents.has(suggestion.on);
}

/** The trigger page's address the suggestion's Add goes to. */
export function suggestionHref(org: string, factory: string, suggestion: Suggestion): string {
  return newTriggerHref(org, factory, sourceByKey[suggestion.source].route, suggestion.on);
}

// The New trigger picker's catalog and the addresses it leads to (ADR 0016 decision 4, ADR 0026 decision 6).
// The schema accepts two GitHub event keys (contracts/definitions/factory.schema.json, the trigger's `on`),
// so only those can be chosen; the other events and the sources not built yet are listed disabled.
import { CircleDot, Clock, Cloud, GitBranch, type LucideIcon, Webhook } from 'lucide-react';

/** A source the picker can start from: its route's `$source` and the trigger's `source` in the API. */
export type SourceKey = 'schedule' | 'webhook' | 'github' | 'cloudflare' | 'slack' | 'linear' | 'jira' | 'sentry';

export interface PickerSource {
  key: SourceKey;
  label: string;
  icon: LucideIcon;
  /** False: listed and disabled, "Not available yet". */
  available: boolean;
  /** Where its choice goes: the trigger's source route segment. */
  route: 'schedule' | 'webhook' | 'github';
}

export const pickerSources: readonly PickerSource[] = [
  { key: 'schedule', label: 'Schedule', icon: Clock, available: true, route: 'schedule' },
  { key: 'webhook', label: 'Webhook', icon: Webhook, available: true, route: 'webhook' },
  { key: 'github', label: 'GitHub', icon: GitBranch, available: true, route: 'github' },
  { key: 'cloudflare', label: 'Cloudflare', icon: Cloud, available: true, route: 'webhook' },
  { key: 'slack', label: 'Slack', icon: CircleDot, available: false, route: 'webhook' },
  { key: 'linear', label: 'Linear', icon: CircleDot, available: false, route: 'webhook' },
  { key: 'jira', label: 'Jira', icon: CircleDot, available: false, route: 'webhook' },
  { key: 'sentry', label: 'Sentry', icon: CircleDot, available: false, route: 'webhook' },
];

/**
 * The picker's groups, top to bottom. Built in sources need no connection. Another source is Connected when
 * the organization has a connection of its kind and the source is built; a source not built yet is never
 * Connected, and only a built one can be connected from here.
 */
export function pickerGroups(
  connectedKinds: ReadonlySet<string>,
): readonly { title: string; keys: readonly SourceKey[] }[] {
  const external = pickerSources.filter((s) => s.key !== 'schedule' && s.key !== 'webhook');
  return [
    { title: 'Built in', keys: ['schedule', 'webhook'] },
    { title: 'Connected', keys: external.filter((s) => s.available && connectedKinds.has(s.key)).map((s) => s.key) },
    {
      title: 'Not connected',
      keys: external.filter((s) => !(s.available && connectedKinds.has(s.key))).map((s) => s.key),
    },
  ];
}

/** The same sources by key, for a choice (the picker and a suggestion name one). */
export const sourceByKey: Record<SourceKey, PickerSource> = Object.fromEntries(
  pickerSources.map((s) => [s.key, s]),
) as Record<SourceKey, PickerSource>;

/** One event the GitHub pane lists: its key when the schema accepts it, else null (disabled). */
export interface PickerEvent {
  label: string;
  key: string | null;
}

export const githubEventGroups: readonly { title: string; events: readonly PickerEvent[] }[] = [
  {
    title: 'Issues',
    events: [
      { label: 'Issue opened', key: null },
      { label: 'Issue labeled', key: 'github.issues.labeled' },
      { label: 'Issue assigned', key: null },
      { label: 'Issue commented', key: null },
    ],
  },
  {
    title: 'Pull requests',
    events: [
      { label: 'Pull request opened', key: null },
      { label: 'Pull request merged', key: null },
      { label: 'Comment on a pull request', key: null },
    ],
  },
  {
    title: 'Checks and code',
    events: [
      { label: 'Check run failed', key: 'github.check_run.failed' },
      { label: 'Push to a branch', key: null },
    ],
  },
];

/** The event keys the schema accepts for a trigger's `on` and the picker can choose. */
export const choosableEvents: ReadonlySet<string> = new Set([
  'github.issues.labeled',
  'github.check_run.failed',
  'schedule',
]);

/** The default choice of the GitHub pane, as the picture has it. */
export const defaultEvent = 'github.check_run.failed';

/**
 * The trigger page's address for a choice (`/o/$org/f/$factory/intake/new/$source?on=<event key>`). A
 * source without an event key (a webhook endpoint is chosen on the trigger page) has no `on`.
 */
export function newTriggerHref(org: string, factory: string, route: PickerSource['route'], on?: string): string {
  const base = `/o/${org}/f/${factory}/intake/new/${route}`;
  return on ? `${base}?${new URLSearchParams({ on })}` : base;
}

// The New trigger picker's catalog and the addresses it leads to (ADR 0016 decision 4, ADR 0026 decision 6).
// The schema accepts two GitHub event keys (contracts/definitions/factory.schema.json, the trigger's `on`),
// so only those can be chosen; the other events and the sources not built yet are listed disabled.
import { Bot, CircleDot, Clock, Cloud, GitBranch, type LucideIcon, Webhook } from 'lucide-react';

/** A source the picker can start from: its route's `$source` and the trigger's `source` in the API. */
export type SourceKey =
  | 'schedule'
  | 'check'
  | 'webhook'
  | 'github'
  | 'cloudflare'
  | 'slack'
  | 'linear'
  | 'jira'
  | 'sentry';

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
  { key: 'check', label: 'Check with an agent', icon: Bot, available: true, route: 'schedule' },
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
  const external = pickerSources.filter((s) => !['schedule', 'check', 'webhook'].includes(s.key));
  return [
    { title: 'Built in', keys: ['schedule', 'check', 'webhook'] },
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

/** The default choice of the GitHub pane, as the picture has it. */
export const defaultEvent = 'github.check_run.failed';

/** A trigger name the definition does not have, from `base`: `new-check`, then `new-check-2`. */
export function freeTriggerName(base: string, taken: (name: string) => boolean): string {
  if (!taken(base)) return base;
  for (let n = 2; ; n++) if (!taken(`${base}-${n}`)) return `${base}-${n}`;
}

/**
 * The trigger page's address for a choice (`/o/$org/f/$factory/intake/<name>?on=<event key>`), which opens
 * a blank trigger. A check adds `check=<agent>`, so the page opens it in the check layout.
 */
export function newTriggerHref(org: string, factory: string, name: string, on: string, check?: string): string {
  const query = new URLSearchParams({ on });
  if (check) query.set('check', check);
  return `/o/${org}/f/${factory}/intake/${name}?${query}`;
}

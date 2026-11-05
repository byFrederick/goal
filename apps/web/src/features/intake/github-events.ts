// The GitHub events a trigger can listen to and the normalized fields each supplies. The one home of this
// table in the web: it mirrors internal/catalog/senders/github.yaml (the Go catalog is the truth, and no
// API serves it yet). A filter on a field an event does not supply is refused when the draft is checked,
// so a drift shows as a problem under the form, never as a wrong save.
import type { TriggerFilter } from '../../shared/definition/model';

/** The filters a form may offer; `skipBots` and `authors` read the sender, so every event supplies them. */
export type FilterKey = Exclude<keyof TriggerFilter, 'match'>;

export interface GithubEvent {
  /** The event's name after `github.`: check_run, pull_request. */
  event: string;
  label: string;
  /** The normalized fields the event supplies, which `filter` reads and a template inserts as {{field}}. */
  fields: string[];
  /** The filters this event supports. */
  filters: FilterKey[];
}

const sender = ['repo', 'author', 'author.type'];

export const githubEvents: GithubEvent[] = [
  {
    event: 'check_run',
    label: 'check_run',
    fields: ['action', 'conclusion', 'check.name', 'check.url', 'branch', 'sha', ...sender],
    filters: ['action', 'conclusion', 'branch', 'repo', 'authors', 'skipBots'],
  },
  {
    event: 'check_suite',
    label: 'check_suite',
    fields: ['action', 'conclusion', 'branch', 'sha', ...sender],
    filters: ['action', 'conclusion', 'branch', 'repo', 'authors', 'skipBots'],
  },
  {
    event: 'workflow_run',
    label: 'workflow_run',
    fields: ['action', 'conclusion', 'workflow.name', 'workflow.url', 'branch', 'sha', ...sender],
    filters: ['action', 'conclusion', 'branch', 'repo', 'authors', 'skipBots'],
  },
  {
    event: 'issues',
    label: 'issues',
    fields: ['action', 'label', 'issue.number', 'issue.title', 'issue.url', ...sender],
    filters: ['action', 'label', 'repo', 'authors', 'skipBots'],
  },
  {
    event: 'issue_comment',
    label: 'issue_comment',
    fields: ['action', 'issue.number', 'issue.title', 'comment.url', ...sender],
    filters: ['action', 'repo', 'authors', 'skipBots'],
  },
  {
    event: 'pull_request',
    label: 'pull_request',
    fields: ['action', 'label', 'pr.number', 'pr.title', 'pr.url', 'branch', 'base', ...sender],
    filters: ['action', 'label', 'branch', 'repo', 'authors', 'skipBots'],
  },
  {
    event: 'pull_request_review',
    label: 'pull_request_review',
    fields: ['action', 'pr.number', 'pr.title', 'review.state', 'branch', ...sender],
    filters: ['action', 'branch', 'repo', 'authors', 'skipBots'],
  },
  {
    event: 'push',
    label: 'push',
    fields: ['branch', 'sha', ...sender, 'paths'],
    filters: ['branch', 'repo', 'paths', 'authors', 'skipBots'],
  },
  {
    event: 'release',
    label: 'release',
    fields: ['action', 'release.tag', 'release.url', ...sender],
    filters: ['action', 'repo', 'authors', 'skipBots'],
  },
];

/** The table's entry for an `on` of `github.<event>`; undefined for another source or an unknown event. */
export function githubEventOf(on: string): GithubEvent | undefined {
  return githubEvents.find((e) => `github.${e.event}` === on);
}

/** Fields a template may insert for `on`: the event's, or none for an event the table lacks. */
export const templateFieldsOf = (on: string): string[] =>
  (githubEventOf(on)?.fields ?? []).filter((f) => f !== 'paths');

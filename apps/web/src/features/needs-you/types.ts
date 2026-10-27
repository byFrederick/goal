// What waits on a person, by the type MASTER.md's vocabulary gives it. Pure, so the mapping is
// tested without a DOM. The API keeps the names (ADR 0016, decision 5): InboxEntryKind's decision is
// a Decision; andon, rework_warning, blocked and expedite are Alerts. Approval and Question have no
// backend yet (ADR 0018 builds questions; tool approvals are not built), so no entry has those types.
import type { InboxEntry } from '../../api/types';

/** The type of an entry the page can show. `generic` is a kind this build does not know. */
export type EntryType = 'decision' | 'alert' | 'generic';

/** A tab of the Needs you page: All, or one type. Approval and question are listed, not built. */
export type NeedsTab = 'all' | 'approval' | 'decision' | 'question' | 'alert';

const alertKinds: ReadonlySet<string> = new Set(['andon', 'rework_warning', 'blocked', 'expedite']);

/** The type of an entry: Decision, Alert, or generic for a kind this build does not know. */
export function typeOf(entry: Pick<InboxEntry, 'kind'>): EntryType {
  if (entry.kind === 'decision') return 'decision';
  if (alertKinds.has(entry.kind)) return 'alert';
  return 'generic';
}

/** The type's word, as the rows and the detail say it. */
export const typeLabel: Record<EntryType, string> = {
  decision: 'Decision',
  alert: 'Alert',
  generic: 'Waiting',
};

/** The tabs in their order, with their labels. */
export const tabs: readonly { value: NeedsTab; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'approval', label: 'Approvals' },
  { value: 'decision', label: 'Decisions' },
  { value: 'question', label: 'Questions' },
  { value: 'alert', label: 'Alerts' },
];

/** Whether an entry shows under a tab. Generic rows show under All only. */
export function inTab(entry: Pick<InboxEntry, 'kind'>, tab: NeedsTab): boolean {
  if (tab === 'all') return true;
  if (tab === 'decision' || tab === 'alert') return typeOf(entry) === tab;
  return false;
}

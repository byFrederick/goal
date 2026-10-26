// The Factories home's sentences (C §10.7): plain language from the overview's numbers. The
// overview's window is a week (7d), so the previous window is "last week".
import type { FactoryOverview } from '../../api/types';
import { formatPercent, formatUsd, formatUsdCompact, plural } from '../../shared/format';

/** "Acme’s", "Example Labs’". */
export function possessive(name: string): string {
  return name.endsWith('s') ? `${name}’` : `${name}’s`;
}

/** The page's description under the h1. */
export function headerSentence(org: string, waiting: number, spend: number, budget: number | null): string {
  const waits =
    waiting === 0
      ? 'Nothing is waiting on you'
      : `${plural(waiting, 'item')} ${waiting === 1 ? 'is' : 'are'} waiting on you`;
  const spent =
    budget === null
      ? `${formatUsd(spend)} was spent this week`
      : `${formatUsd(spend)} of the ${formatUsdCompact(budget)} weekly budget is spent`;
  return `${possessive(org)} factories. ${waits}, and ${spent}.`;
}

/** A work type or connection as a counted noun: "2 changes", "3 policies", "1 runbook draft". */
function countOf(n: number, id: string): string {
  const one = id.replace(/[-_]+/g, ' ').trim().toLowerCase();
  const many = /[^aeiou]y$/.test(one) ? `${one.slice(0, -1)}ies` : /(s|x|z|ch|sh)$/.test(one) ? `${one}es` : `${one}s`;
  return plural(n, one, many);
}

/** What a factory's "Waiting on you" stands for: tool approvals by connection, then items by type. */
export function waitingBreakdown(waiting: FactoryOverview['waiting']): string {
  const parts = [
    ...waiting.tool_approvals.map((t) => `${countOf(t.count, `${t.connection} approval`)}`),
    ...waiting.items_by_type.map((t) => countOf(t.count, t.type || 'item')),
  ];
  return parts.length > 0 ? parts.join(', ') : 'Nothing right now';
}

/** The phone banner's second line: the one factory that waits, else the count of each. */
export function bannerDetail(
  factories: readonly { factory: { name: string }; waiting: FactoryOverview['waiting'] }[],
): string {
  const waiting = factories.filter((f) => f.waiting.on_you > 0);
  const [only] = waiting;
  if (waiting.length === 1 && only) return `All in ${only.factory.name}: ${waitingBreakdown(only.waiting)}`;
  return waiting.map((f) => `${f.factory.name} ${f.waiting.on_you}`).join(', ');
}

export interface Delta {
  /** Always signed or worded, so the trend never rests on colour. */
  text: string;
  trend: 'up' | 'down' | 'flat';
}

/** Throughput a day against the previous window, to one decimal. */
export function throughputDelta(perDay: number, previousPerDay: number): Delta {
  const delta = Math.round((perDay - previousPerDay) * 10) / 10;
  if (delta === 0) return { text: 'Same as last week', trend: 'flat' };
  const sign = delta > 0 ? '+' : '−';
  return { text: `${sign}${Math.abs(delta).toFixed(1)} vs last week`, trend: delta > 0 ? 'up' : 'down' };
}

/** A change in touches per finished item: fewer is better. Signed, so it never rests on colour. */
export interface TouchesTrend {
  /** "+0.3", "−0.1", or "Same". */
  text: string;
  tone: 'better' | 'worse' | 'flat';
}

/**
 * Touches per finished item this week against last week, to one decimal; null when either week
 * finished nothing, as there is no per item to compare.
 */
export function touchesTrend({ current, previous }: FactoryOverview['touches']): TouchesTrend | null {
  if (current.finished === 0 || previous.finished === 0) return null;
  const delta = Math.round((current.per_item_mean - previous.per_item_mean) * 10) / 10;
  if (delta === 0) return { text: 'Same', tone: 'flat' };
  return { text: `${delta > 0 ? '+' : '−'}${Math.abs(delta).toFixed(1)}`, tone: delta > 0 ? 'worse' : 'better' };
}

/** The line under the constraint's chip. */
export function constraintNote(constraint: FactoryOverview['constraint']): string {
  if (!constraint) return 'None right now';
  if (constraint.busy === null) return 'Most work per slot now';
  return `Busy ${formatPercent(constraint.busy)} this week`;
}

/** The line under a factory's spend, and the meter's accessible name (null: no meter). */
export function spendNote(spend: number, budget: number | null): { text: string; label: string | null } {
  if (budget === null) return { text: 'No budget set', label: null };
  const of = formatUsdCompact(budget);
  const share = budget > 0 ? formatPercent(spend / budget) : '100%';
  return {
    text: spend > budget ? `${formatUsdCompact(spend - budget)} over the ${of} budget` : `of ${of} budget`,
    label: `${formatUsd(spend)} of ${of} weekly budget, ${share}`,
  };
}

/** The phone page's last line. */
export function budgetLine(org: string, spend: number, budget: number | null): string {
  return budget === null
    ? `${formatUsd(spend)} spent this week, no budget set`
    : `${formatUsd(spend)} of ${possessive(org)} ${formatUsdCompact(budget)} weekly budget spent`;
}

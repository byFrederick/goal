// Jonah's copy and numbers, from the overview and the proposals (ADR 0034). Pure, so the rules are tested
// without a page.
import type {
  JonahPolicy,
  ProposalEstimate,
  ProposalFile,
  ProposalState,
  ProposalTried,
  TriedItem,
  TriedTry,
  Watch,
  WatchVerdict,
} from '../../api/types';
import { formatPercent, formatUsd, plural } from '../../shared/format';

/** A proposal's state in words, for the Proposals tab's filter and a card's chip. */
export const proposalStateLabels: Record<ProposalState, string> = {
  proposed: 'Waiting',
  published: 'Published',
  applied: 'Applied',
  rejected: 'Rejected',
  superseded: 'Superseded',
};

const roleNames: Record<string, string> = {
  owner: 'owners',
  admin: 'admins',
  editor: 'editors',
  viewer: 'viewers',
  member: 'members',
};

/** "Owners and admins", "Owners, admins and editors": the roles that may approve, in the order the policy gives. */
export function reviewersText(roles: readonly string[]): string {
  const names = roles.map((role) => roleNames[role] ?? role);
  const last = names.pop();
  if (last === undefined) return 'Nobody yet';
  const text = names.length === 0 ? last : `${names.join(', ')} and ${last}`;
  return text.replace(/^./, (first) => first.toUpperCase());
}

/** The model the review runs on, or "Not set" when neither the policy nor the runtime sets one. */
export function modelText(policy: JonahPolicy): string {
  return policy.model ?? 'Not set';
}

/** "Proposes after" in the policy's words: "5 failures with the same cause". */
export function proposeAfterText(policy: JonahPolicy): string {
  return `${plural(policy.propose_after, 'failure', 'failures')} with the same cause`;
}

/** Hours with no decimals, as the estimate shows them: "31 h". */
function hours(h: number): string {
  return `${Math.round(h)} h`;
}

/** The estimate's metric in words and its before and after, as the card's line shows them. */
export function estimateOf(estimate: ProposalEstimate): { label: string; before: string; after: string } {
  if (estimate.metric === 'sent_back_rate') {
    return {
      label: estimate.station ? `Sent back, ${estimate.station} lane` : 'Sent back',
      before: formatPercent(estimate.before),
      after: formatPercent(estimate.after),
    };
  }
  return { label: 'Lead time, p85', before: hours(estimate.before), after: hours(estimate.after) };
}

/** The estimate's change as the stat strip's delta: "−8 pts" for a rate, "−5 h" for a time; good when it goes down. */
export function estimateDelta(estimate: ProposalEstimate): { text: string; good: boolean } {
  const change =
    estimate.metric === 'sent_back_rate' ? (estimate.after - estimate.before) * 100 : estimate.after - estimate.before;
  const rounded = Math.round(change);
  const unit = estimate.metric === 'sent_back_rate' ? 'pts' : 'h';
  const sign = rounded > 0 ? '+' : rounded < 0 ? '−' : '';
  return { text: `${sign}${Math.abs(rounded)} ${unit}`, good: rounded <= 0 };
}

/** The cost per item the change adds: "+$0.04", "-$0.02", or "no change". */
export function costPerItemText(usd: number): string {
  if (usd === 0) return 'no change';
  return usd > 0 ? `+${formatUsd(usd)}` : formatUsd(usd);
}

/** "Based on 4 runs sent back": the evidence a proposal rests on. */
export function basedOnText(runs: number): string {
  return `Based on ${plural(runs, 'run', 'runs')} sent back`;
}

/** An observation's chip: Proposed once it became a proposal, else how far it has come. */
export function observationChip(state: 'watching' | 'proposed' | 'not_jonahs', count: number, proposeAfter: number) {
  if (state === 'not_jonahs') return "Not Jonah's";
  return state === 'proposed' ? 'Proposed' : `Watching, ${count} of ${proposeAfter}`;
}

/** The short ref of an item, its number with a hash, from the ref with the factory name before it; an id of a removed item stays as it is. */
export function itemNumber(ref: string): string {
  const hash = ref.lastIndexOf('#');
  return hash < 0 ? ref : ref.slice(hash);
}

/** What a proposal is: a change to the definition, to the factory's memory, or to an agent's model (tried three times). */
export type ProposalKind = 'definition' | 'memory' | 'model';

/** The kind of a proposal from the paths it edits and what was tried: only a model proposal's items carry the line's result. */
export function proposalKind(p: { paths: readonly string[]; tried?: ProposalTried | undefined }): ProposalKind {
  if (p.paths.length === 1 && p.paths[0] === 'memory.yaml') return 'memory';
  if (p.tried?.items.some((item) => item.line_result !== undefined)) return 'model';
  return 'definition';
}

/** A try's verdict in words, as the Tried on past items rows say it. */
export const verdictLabels: Record<NonNullable<TriedTry['verdict']>, string> = {
  fixed: 'Fixed',
  not_fixed: 'Not fixed',
  unknown: 'Unknown',
  error: 'Error',
};

/** The tone of a verdict: fixed is ok, not fixed and error are sent back, unknown is quiet. */
export const verdictTones: Record<NonNullable<TriedTry['verdict']>, 'ok' | 'rework' | 'plain'> = {
  fixed: 'ok',
  not_fixed: 'rework',
  unknown: 'plain',
  error: 'rework',
};

/** An item's verdict when it was tried once: the verdict of its try, or undefined while that try waits. */
export function itemVerdict(item: TriedItem): TriedTry['verdict'] {
  return item.tries[0]?.verdict;
}

/** Why an item's try was graded as it was, or why it could not be carried out; undefined when it says nothing. */
export function itemReason(item: TriedItem): string | undefined {
  const t = item.tries[0];
  return t?.verdict === 'error' ? (t.error ?? t.reason) : t?.reason;
}

/** How many tried items a try of a definition proposal fixed, of those tried. */
export function fixedCount(tried: ProposalTried): { fixed: number; total: number } {
  return { fixed: tried.items.filter((item) => itemVerdict(item) === 'fixed').length, total: tried.items.length };
}

/** Kept tests that passed, of those run. */
export function keptCount(tried: ProposalTried): { passed: number; total: number } {
  return { passed: tried.kept_tests.filter((t) => t.result === 'passed').length, total: tried.kept_tests.length };
}

/** For a model proposal: the tries that passed and all tries. A try passes when it is graded fixed. */
export function triesPassed(tried: ProposalTried): { passed: number; total: number } {
  const tries = tried.items.flatMap((item) => item.tries);
  return { passed: tries.filter((t) => t.verdict === 'fixed').length, total: tries.length };
}

/** Whether the item passed first time on the line, from its line result ("passed first time, $0.42"). */
export function passedFirstTime(item: TriedItem): boolean {
  return item.line_result?.startsWith('passed first time') ?? false;
}

/** How many of a model proposal's items passed first time on the line, of those that have a line result. */
export function lineCount(tried: ProposalTried): { passed: number; total: number } {
  const items = tried.items.filter((item) => item.line_result !== undefined);
  return { passed: items.filter(passedFirstTime).length, total: items.length };
}

/** What the tried items cost on the line, averaged from their line results ("$0.42"); undefined when none says. */
export function lineCostPerItem(tried: ProposalTried): number | undefined {
  const costs = tried.items.flatMap((item) => {
    const match = /\$(\d+(?:\.\d+)?)/.exec(item.line_result ?? '');
    return match?.[1] ? [Number(match[1])] : [];
  });
  return costs.length === 0 ? undefined : costs.reduce((sum, c) => sum + c, 0) / costs.length;
}

/** What one try cost on average over the tries that ran; undefined before any ran. */
export function costPerTry(tried: ProposalTried): number | undefined {
  const tries = tried.items.flatMap((item) => item.tries).filter((t) => t.state === 'done');
  return tries.length === 0 ? undefined : tried.cost_usd / tries.length;
}

/** "41 min", "1 h 05 min", "30 s": how long the tries took. */
export function durationText(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)} s`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes} min`;
  return `${Math.floor(minutes / 60)} h ${String(minutes % 60).padStart(2, '0')} min`;
}

/** The tried line of a waiting proposal's card: "Tried: fixed 4 of 5, kept tests 2 of 2", or undefined when nothing was tried. */
export function triedLine(tried: ProposalTried | undefined, kind: ProposalKind): string | undefined {
  if (!tried || kind === 'memory') return undefined;
  if (tried.state === 'budget_used' && tried.items.every((item) => item.tries.every((t) => t.verdict === undefined)))
    return 'Not tried: budget used';
  if (kind === 'model') {
    const { passed, total } = triesPassed(tried);
    return `Tried: ${passed} of ${total} tries passed`;
  }
  const { fixed, total } = fixedCount(tried);
  const kept = keptCount(tried);
  return `Tried: fixed ${fixed} of ${total}${kept.total > 0 ? `, kept tests ${kept.passed} of ${kept.total}` : ''}`;
}

/** The facts a memory proposal adds: each `- text:` entry of memory.yaml that the base lacks, with the refs of its `from:`. */
export function addedFacts(base: string, content: string): { text: string; from: string[] }[] {
  const factsOf = (yaml: string) =>
    [...yaml.matchAll(/^- text:\s*(.+)\n\s*from:\s*\[([^\]]*)\]/gm)].map((m) => ({
      text: (m[1] ?? '').trim(),
      from: (m[2] ?? '')
        .split(',')
        .map((ref) => ref.trim())
        .filter((ref) => ref !== ''),
    }));
  const had = new Set(factsOf(base).map((fact) => fact.text));
  return factsOf(content).filter((fact) => !had.has(fact.text));
}

/** The number of facts in a memory.yaml. */
export function factCount(yaml: string): number {
  return (yaml.match(/^- text:/gm) ?? []).length;
}

/** The model a model proposal sets: the last `model:` line a file adds. */
export function modelOfProposal(files: readonly ProposalFile[]): string | undefined {
  for (const file of [...files].reverse()) {
    const before = new Set(file.base_content.split('\n'));
    const added = file.content.split('\n').filter((line) => !before.has(line));
    const match = added.map((line) => /^\s*model:\s*(\S+)/.exec(line)).find((m) => m !== null);
    if (match?.[1]) return match[1];
  }
  return undefined;
}

/** A watch's metric in words: "Sent back for a criterion", "Cost per item", "Lead time p85". */
export function watchMetricLabel(metric: Watch['metric']): string {
  return { sent_back_rate: 'Send-backs', cost_per_item: 'Cost per item', lead_time_p85: 'Lead time p85' }[metric];
}

/** One side of a watch as its metric shows it: "31 h", "$1.91", "20%". */
export function watchValue(metric: Watch['metric'], value: number): string {
  if (metric === 'sent_back_rate') return formatPercent(value);
  if (metric === 'cost_per_item') return formatUsd(value);
  return hours(value / 3600);
}

/** "Lead time p85: 31 h → 34 h" for a watch. */
export function watchText(watch: Watch): string {
  const { metric } = watch;
  return `${watchMetricLabel(metric)} at ${watch.station}: ${watchValue(metric, watch.before.value)} → ${watchValue(metric, watch.after.value)}`;
}

/** A watch's verdict in words and tone. Worse is sent back; Helped is ok; the rest wait or say nothing changed. */
export const watchVerdicts: Record<WatchVerdict, { label: string; tone: 'ok' | 'rework' | 'plain' }> = {
  too_early: { label: 'Too early', tone: 'plain' },
  helped: { label: 'Helped', tone: 'ok' },
  worse: { label: 'Worse', tone: 'rework' },
  no_change: { label: 'No change', tone: 'plain' },
  no_clear_change: { label: 'No clear change', tone: 'plain' },
};

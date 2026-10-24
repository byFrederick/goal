// How the Floor's constraint card says a candidate: its headline, the queue in front of it, what
// one more slot would save. Agent and station names come from the factory.
import type { ConstraintCandidate } from '../../api/types';
import { humanize, plural } from '../../shared/format';
import { daysIn } from './range';

const numbers = ['no', 'one', 'two', 'three', 'four', 'five', 'six', 'seven', 'eight', 'nine', 'ten'];
const ordinals = ['first', 'second', 'third', 'fourth', 'fifth', 'sixth', 'seventh', 'eighth', 'ninth', 'tenth'];

/** "an investigator", "a reviewer". */
const article = (noun: string) => `${/^[aeiou]/i.test(noun) ? 'an' : 'a'} ${noun}`;

/** The agent of a station with one, else "agent". */
const agentOf = (c: ConstraintCandidate) => (c.agents.length === 1 ? (c.agents[0] ?? 'agent') : 'agent');

/** "Investigate is holding the line back" for the constraint, the others by their place. */
export function headline(c: ConstraintCandidate, index: number, constraint: string | null): string {
  const name = humanize(c.station);
  if (c.station === constraint) return `${name} is holding the line back`;
  return index === 1 ? `${name} is the next busiest` : `${name} is the ${ordinals[index] ?? `${index + 1}th`} busiest`;
}

/** "On average two items wait for an investigator." */
export function waitingSentence(c: ConstraintCandidate): string {
  const n = Math.round(c.avg_waiting);
  if (n === 0) return 'Almost nothing waits in front of it.';
  const count = numbers[n] ?? `${n}`;
  return `On average ${count} ${n === 1 ? 'item waits' : 'items wait'} for ${article(agentOf(c))}.`;
}

/** "A fourth investigator would cut lead time by about 2 hours.", or null without an estimate. */
export function estimateSentence(c: ConstraintCandidate): string | null {
  if (!c.estimate) return null;
  const saved = c.estimate.extra_slot_saves_hours;
  const amount = saved >= 1 ? plural(Math.round(saved), 'hour') : plural(Math.max(1, Math.round(saved * 60)), 'minute');
  const nth = ordinals[c.slots] ?? `${c.slots + 1}th`;
  return `A ${nth} ${agentOf(c)} would cut lead time by about ${amount}.`;
}

/** "Add an investigator". */
export function addLabel(c: ConstraintCandidate): string {
  return `Add ${article(agentOf(c))}`;
}

/** The stretch busy shares cover: "7 days". */
export function busyOver(from: string, to: string): string {
  return plural(daysIn(from, to), 'day');
}

// The nine rows of "Finish setting up" on the Floor (ADR 0032 decision 2). Each row is done when the
// data the factory already has says so: nothing is stored for it. This file is a pure function over
// the answers the card's queries give, so the rules are tested without a request.
import type { Connection, IntakeTrigger, Invitation, KnowledgeSource, Member } from '../../api/types';
import type { Model } from '../../shared/definition/model';

/** How many rows the checklist has; the ring and "N of 9 done" count against it. */
export const setupTotal = 9;

export type SetupRowId =
  | 'invite'
  | 'slack'
  | 'knowledge'
  | 'organization'
  | 'github'
  | 'line'
  | 'budget'
  | 'triggers'
  | 'first-item';

export interface SetupInput {
  members: Member[];
  invitations: Invitation[];
  /** The organization's connections, every kind. */
  connections: Connection[];
  /** The organization's knowledge sources. */
  knowledge: KnowledgeSource[];
  /** The published version's model (the section values the definition editor reads). */
  model: Model;
  /** The factory's triggers as intake reports them. */
  triggers: IntakeTrigger[];
  /** How many of the factory's items are done (merged); the Floor asks for one, so 0 or 1 matter. */
  finishedItems: number;
  /** The factory's name, for the Slack row's hint. */
  factoryName: string;
}

export interface SetupRow {
  id: SetupRowId;
  /** The row's headline, with its count where it has one. */
  text: string;
  /** The line under a row that is to do: what it gets the factory. */
  hint: string | null;
  done: boolean;
}

/** "1 repository" and "2 repositories": the noun for a count. */
export function plural(count: number, one: string, many: string): string {
  return `${count} ${count === 1 ? one : many}`;
}

/** The rows in the picture's order. Stations are the line's stations, and Done is not counted. */
export function setupRows(input: SetupInput): SetupRow[] {
  const repositories = input.model.basics?.repository ? 1 : 0;
  const stations = (input.model.line?.stations ?? []).filter((station) => station.kind !== 'done').length;
  const budget = input.model.review?.budget ?? {};
  const targets = input.model.line?.flow?.targets ?? {};
  const budgetSet =
    [budget.weekly_usd, budget.daily_usd, budget.per_item_usd].some((value) => value !== undefined) ||
    Object.values(targets).some((value) => value !== undefined);
  // Every trigger the factory has counts, on or off: a switched-off trigger is still set up, only waiting.
  const triggers = input.triggers.length;
  const hasGitHub = input.connections.some((connection) => connection.kind === 'github');

  return [
    {
      id: 'invite',
      text: 'Invite people',
      hint: 'Someone to approve writes when you are away',
      done: input.members.length > 1 || input.invitations.length > 0,
    },
    {
      id: 'slack',
      text: 'Connect Slack',
      hint: `Ask @${input.factoryName} from any channel`,
      done: input.connections.some((connection) => connection.kind === 'slack'),
    },
    {
      id: 'knowledge',
      text: 'Add knowledge',
      hint: 'Docs and decisions agents read first',
      done: input.knowledge.length > 0,
    },
    { id: 'organization', text: 'Organization', hint: null, done: true },
    {
      id: 'github',
      text: `GitHub, ${plural(repositories, 'repository', 'repositories')}`,
      hint: 'Connect the repositories agents work in',
      done: hasGitHub,
    },
    {
      id: 'line',
      text: `The line, ${plural(stations, 'station', 'stations')}`,
      hint: 'Stations and the agents that work them',
      done: stations > 0,
    },
    {
      id: 'budget',
      text: 'Budgets and targets',
      hint: 'A spend cap and the flow you aim for',
      done: budgetSet,
    },
    {
      id: 'triggers',
      text: plural(triggers, 'trigger', 'triggers'),
      hint: 'Where work comes from, on or off',
      done: triggers > 0,
    },
    {
      id: 'first-item',
      text: 'First item merged',
      hint: 'Send one item through the line',
      done: input.finishedItems > 0,
    },
  ];
}

/** How many rows are done. */
export function doneCount(rows: readonly SetupRow[]): number {
  return rows.filter((row) => row.done).length;
}

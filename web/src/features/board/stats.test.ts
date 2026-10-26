import { describe, expect, it } from 'vitest';
import type { BoardLine, BoardStation, ItemCard } from '../../api/types';
import {
  budgetNote,
  cardState,
  columnCount,
  constraintSentence,
  doingText,
  expediteHeader,
  expediteUse,
  firstStation,
  isExpedite,
  lineSentence,
  stationMeter,
  stationNote,
  stationStat,
  waitStatus,
  wipText,
} from './stats';

const station = (over: Partial<BoardStation>): BoardStation => ({
  id: 'work',
  kind: 'agent',
  agents: ['worker'],
  wip_limit: null,
  waiting: 0,
  in_progress: 0,
  items: [],
  busy_ratio: null,
  send_back_ratio: null,
  queue_trend: null,
  oldest_waiting_seconds: null,
  done_today: null,
  ...over,
});

const waitingHuman = { state: 'waiting_human' } as ItemCard;

describe('stationStat', () => {
  it('counts an agent station’s slots in use and what is ready', () => {
    expect(stationStat(station({ in_progress: 3, wip_limit: 3, waiting: 2 }))).toBe('3 of 3, 2 ready');
    expect(stationStat(station({ in_progress: 1, wip_limit: 4 }))).toBe('1 of 4');
    expect(stationStat(station({ in_progress: 2 }))).toBe('2 busy');
  });

  it('counts what waits on you against a human station’s limit', () => {
    const items = [waitingHuman, waitingHuman];
    expect(stationStat(station({ kind: 'human', wip_limit: 5, items }))).toBe('2 of 5');
    expect(stationStat(station({ kind: 'human', items }))).toBe('2 waiting');
  });

  it('counts a queue’s waiting items and the items done today', () => {
    expect(stationStat(station({ kind: 'queue', waiting: 7 }))).toBe('7 waiting');
    expect(stationStat(station({ kind: 'done', done_today: 4 }))).toBe('4 today');
  });
});

describe('stationNote', () => {
  it('tells how long the oldest item has waited in a queue', () => {
    expect(stationNote(station({ kind: 'queue', waiting: 7, oldest_waiting_seconds: 7800 }))).toBe('Oldest 2 h 10 min');
    expect(stationNote(station({ kind: 'queue' }))).toBe('Empty');
  });

  it('shows a mostly idle agent station by its idle share, a busy one with its queue’s trend', () => {
    expect(stationNote(station({ busy_ratio: 0.38 }))).toBe('Idle 62% of the time');
    expect(stationNote(station({ busy_ratio: 0.94, queue_trend: 'growing' }))).toBe('Busy 94%, queue growing');
    expect(stationNote(station({ busy_ratio: 0.5, queue_trend: 'steady' }))).toBe('Busy 50%');
    expect(stationNote(station({ busy_ratio: 0.7, queue_trend: 'shrinking' }))).toBe('Busy 70%, queue shrinking');
  });

  it('prefers a gate’s send-back share, and names the agents without history', () => {
    expect(stationNote(station({ busy_ratio: 0.5, send_back_ratio: 0.18 }))).toBe('Sends back 18%');
    expect(stationNote(station({ agents: ['triager', 'reviewer'] }))).toBe('triager, reviewer');
  });

  it('says whether a human station waits on you; done says nothing more', () => {
    expect(stationNote(station({ kind: 'human', items: [waitingHuman] }))).toBe('Waiting on you');
    expect(stationNote(station({ kind: 'human' }))).toBe('Nothing waiting');
    expect(stationNote(station({ kind: 'done', done_today: 4 }))).toBe('');
  });

  it('says a decision waits on the approvers to someone who does not decide', () => {
    expect(stationNote(station({ kind: 'human', items: [waitingHuman] }), false)).toBe('Waiting on approvers');
    expect(stationNote(station({ kind: 'human' }), false)).toBe('Nothing waiting');
  });
});

describe('columnCount', () => {
  it('heads a column with its slots in use, what waits on you, the queue or today’s done items', () => {
    expect(columnCount(station({ in_progress: 3, wip_limit: 3, waiting: 2 }))).toBe('3 of 3');
    expect(columnCount(station({ kind: 'human', wip_limit: 5, items: [waitingHuman, waitingHuman] }))).toBe('2 of 5');
    expect(columnCount(station({ kind: 'queue', waiting: 7 }))).toBe('7');
    expect(columnCount(station({ kind: 'done', done_today: 4 }))).toBe('4 today');
  });
});

describe('stationMeter', () => {
  it('fills an agent station’s meter with its slots in use, rust when its gate sends work back', () => {
    expect(stationMeter(station({ in_progress: 1, wip_limit: 4 }))).toEqual({ value: 1, max: 4, tone: 'run' });
    expect(stationMeter(station({ in_progress: 1, wip_limit: 2, send_back_ratio: 0.18 }))).toEqual({
      value: 1,
      max: 2,
      tone: 'rework',
    });
  });

  it('fills a human station’s with what waits on you; queues and done stay empty', () => {
    expect(stationMeter(station({ kind: 'human', wip_limit: 5, items: [waitingHuman, waitingHuman] }))).toEqual({
      value: 2,
      max: 5,
      tone: 'run',
    });
    expect(stationMeter(station({ kind: 'queue', waiting: 7 })).value).toBe(0);
    expect(stationMeter(station({ kind: 'done', done_today: 4 })).value).toBe(0);
  });
});

const line = (over: Partial<BoardLine> = {}): BoardLine => ({
  status: 'running',
  andon: null,
  wip: { in_progress: 9, limit: 15 },
  release: { policy: 'push', enforced: false, drum: 'investigate', buffer: 2, in_buffer: 2 },
  expedite: null,
  budget: null,
  touches: { finished: 0, per_item_mean: 0, per_item_median: 0, touchless_rate: 0 },
  ...over,
});

describe('lineSentence', () => {
  it('states the rope of drum-buffer-rope, not enforced yet', () => {
    expect(
      lineSentence(
        line({
          release: { policy: 'drum-buffer-rope', enforced: false, drum: 'investigate', buffer: 2, in_buffer: 1 },
        }),
      ),
    ).toEqual({ text: 'New work enters only when Investigate’s buffer drops below 2', notEnforced: true });
  });

  it('names the drum of a push line, or says nothing holds it back', () => {
    expect(lineSentence(line())).toEqual({
      text: 'Investigate is the constraint; the line moves at its pace.',
      notEnforced: false,
    });
    expect(
      lineSentence(line({ release: { policy: 'push', enforced: false, drum: null, buffer: null, in_buffer: 0 } })).text,
    ).toBe('No station holds the line back right now.');
  });

  it('falls back to the drum when drum-buffer-rope has no buffer to keep', () => {
    expect(
      lineSentence(
        line({ release: { policy: 'drum-buffer-rope', enforced: false, drum: 'build', buffer: null, in_buffer: 0 } }),
      ),
    ).toEqual({ text: 'Build is the constraint; the line moves at its pace.', notEnforced: false });
  });
});

describe('wipText and expediteUse', () => {
  it('counts work in progress against the stations’ limits', () => {
    expect(wipText(line())).toBe('WIP 9 of 15');
    expect(wipText(line({ wip: { in_progress: 3, limit: null } }))).toBe('WIP 3');
  });

  it('counts the expedite lane’s use against its limit', () => {
    expect(expediteUse(line({ expedite: { limit: 1, in_use: 1 } }), 1)).toBe('1 of 1 in use');
    expect(expediteUse(line({ expedite: { limit: null, in_use: 2 } }), 2)).toBe('2 in use');
    expect(expediteUse(line(), 3)).toBe('3 items skipping the queue');
  });

  it('heads the line with the expedite count only when there is a lane or an expedite item', () => {
    expect(expediteHeader(line({ expedite: { limit: 1, in_use: 1 } }), 1)).toBe('Expedite 1 of 1 in use');
    expect(expediteHeader(line(), 2)).toBe('Expedite 2 items skipping the queue');
    expect(expediteHeader(line(), 0)).toBeNull();
  });

  it('marks an expedite item only while it is live', () => {
    expect(isExpedite({ class_of_service: 'expedite', state: 'in_progress' })).toBe(true);
    expect(isExpedite({ class_of_service: 'expedite', state: 'blocked' })).toBe(true);
    expect(isExpedite({ class_of_service: 'expedite', state: 'done' })).toBe(false);
    expect(isExpedite({ class_of_service: 'expedite', state: 'cancelled' })).toBe(false);
    expect(isExpedite({ class_of_service: 'standard', state: 'ready' })).toBe(false);
  });
});

describe('budgetNote', () => {
  it('speaks only when the weekly budget is used up', () => {
    expect(budgetNote(line({ budget: { weekly_usd: 60, spent_usd: 61.2, exhausted: true } }))).toBe(
      'Weekly budget used up: $61.20 of $60. No new agent work starts until the week’s spend falls below it.',
    );
    expect(budgetNote(line({ budget: { weekly_usd: 60, spent_usd: 12, exhausted: false } }))).toBeNull();
    expect(budgetNote(line())).toBeNull();
  });
});

describe('constraintSentence', () => {
  it('names the station and its slots in use against its limit, and what waits', () => {
    expect(constraintSentence(station({ id: 'build', wip_limit: 6, in_progress: 2, waiting: 3 }))).toBe(
      'Build is the constraint: 2 of 6 busy, 3 waiting',
    );
  });

  it('says busy without a limit', () => {
    expect(constraintSentence(station({ id: 'build', wip_limit: null, in_progress: 2, waiting: 0 }))).toBe(
      'Build is the constraint: 2 busy, 0 waiting',
    );
  });
});

const card = (over: Partial<ItemCard>): ItemCard => ({
  id: 'i',
  ref: 'sre#1',
  number: 1,
  title: 't',
  type: 'incident',
  station: 'work',
  state: 'queued',
  class_of_service: 'standard',
  rework_count: 0,
  cost_usd: 0,
  created_at: '',
  updated_at: '',
  age_seconds: 0,
  agent: null,
  pr_url: null,
  pull_requests: [],
  slot: null,
  turn: null,
  summary: null,
  in_buffer: false,
  rope_held: false,
  touch_count: 0,
  ...over,
});

describe('cardState', () => {
  it('says working and its turn while an item is worked, never a numbered agent copy', () => {
    expect(cardState(card({ state: 'in_progress', agent: 'investigator', slot: 1, turn: 22 }))).toEqual({
      text: 'Working, turn 22',
      tone: 'run',
    });
    expect(cardState(card({ state: 'in_progress', agent: 'triager', turn: 0 })).text).toBe('Working');
  });

  it('asks for your OK, says how often work was sent back, and what a done item became', () => {
    expect(cardState(card({ state: 'waiting_human' }))).toEqual({ text: 'Needs your OK', tone: 'you' });
    expect(cardState(card({ state: 'waiting_human' }), false)).toEqual({
      text: 'Waiting on an approver',
      tone: 'muted',
    });
    expect(cardState(card({ rework_count: 1 }))).toEqual({ text: 'Sent back once', tone: 'rework' });
    expect(cardState(card({ rework_count: 2 })).text).toBe('Sent back twice');
    expect(cardState(card({ rework_count: 3 })).text).toBe('Sent back 3 times');
    expect(cardState(card({ state: 'done', summary: 'Postmortem filed' }))).toEqual({
      text: 'Postmortem filed',
      tone: 'ok',
    });
    expect(cardState(card({ state: 'done' })).text).toBe('Done');
  });

  it('shows a done item’s summary as a short excerpt: its first line, without markdown marks', () => {
    const summary = [
      '## Added the version subcommand',
      '',
      '- **Added** `cmd/goal/version.go` with the `version` subcommand.',
      '- Wired it in `cmd/goal/main.go`, next to `help`.',
      '',
      '1. Tests cover the output format.',
      '2. The commit comes from the build info.',
      '',
      '> Notes for the reviewer below.',
      ...Array.from({ length: 16 }, (_, i) => `- Detail ${i + 1} of the migration and the port adapter.`),
    ].join('\n');
    expect(cardState(card({ state: 'done', summary }))).toEqual({
      text: 'Added the version subcommand',
      tone: 'ok',
    });
    expect(cardState(card({ state: 'done', summary: '  \n\n- **Bold** `code` text\n' })).text).toBe('Bold code text');
  });

  it('cuts a long first line at a word boundary, to at most 100 characters with an ellipsis', () => {
    const long = `${'Migration and port detail for the version command, '.repeat(3)}end`;
    const text = cardState(card({ state: 'done', summary: long })).text;
    expect(text.endsWith('…')).toBe(true);
    expect(Array.from(text).length).toBeLessThanOrEqual(100);
    expect(long.startsWith(text.slice(0, -1))).toBe(true);
    expect(text.slice(0, -1).endsWith(' ')).toBe(false);
  });

  it('keeps a multi-byte character whole when it cuts', () => {
    const text = cardState(card({ state: 'done', summary: `${'é🙂'.repeat(60)}` })).text;
    expect(text.endsWith('…')).toBe(true);
    expect(Array.from(text).length).toBeLessThanOrEqual(100);
    expect(
      Array.from(text)
        .slice(0, -1)
        .every((c) => c === 'é' || c === '🙂'),
    ).toBe(true);
  });

  it('reads Done for a done item whose summary has no words', () => {
    expect(cardState(card({ state: 'done', summary: '' })).text).toBe('Done');
    expect(cardState(card({ state: 'done', summary: '\n   \n\t\n' })).text).toBe('Done');
    expect(cardState(card({ state: 'done', summary: '## \n- ' })).text).toBe('Done');
  });

  it('marks work in the drum’s buffer, and work the rope holds back', () => {
    expect(cardState(card({ state: 'ready', in_buffer: true })).text).toBe('Ready in buffer');
    expect(cardState(card({ rework_count: 1, in_buffer: true })).text).toBe('Ready in buffer');
    expect(cardState(card({ rope_held: true }))).toEqual({ text: 'Waiting for buffer room', tone: 'muted' });
    expect(cardState(card({})).text).toBe('Waiting');
  });
});

describe('firstStation', () => {
  const card = (state: ItemCard['state']) => ({ state }) as ItemCard;
  const inbox = station({ id: 'inbox', kind: 'queue', items: [card('queued')] });
  const build = station({ id: 'build', items: [card('in_progress')] });
  const fixes = station({ id: 'fixes', items: [card('blocked')] });
  const approve = station({ id: 'approve', kind: 'human', items: [card('waiting_human')] });
  const done = station({ id: 'done', kind: 'done' });

  it('opens where items wait on a person first, then where they are blocked', () => {
    expect(firstStation({ stations: [inbox, build, fixes, approve, done], constraint: 'build' })).toBe('approve');
    expect(firstStation({ stations: [inbox, build, fixes, done], constraint: 'build' })).toBe('fixes');
  });

  it('else opens the constraint, else the first station', () => {
    expect(firstStation({ stations: [inbox, build, done], constraint: 'build' })).toBe('build');
    expect(firstStation({ stations: [station({ id: 'inbox', kind: 'queue' }), done], constraint: null })).toBe('inbox');
    expect(firstStation({ stations: [], constraint: null })).toBe('');
  });
});

describe('waitStatus and doingText', () => {
  it('shows a check for a ready item and a clock for one that waits', () => {
    expect(waitStatus(card({ state: 'ready' }))).toBe('ready');
    expect(waitStatus(card({ state: 'ready', in_buffer: true }))).toBe('waiting');
    expect(waitStatus(card({ state: 'queued' }))).toBe('waiting');
    expect(waitStatus(card({ state: 'blocked' }))).toBeNull();
  });

  it('says the doing line with the turn, never a numbered agent', () => {
    expect(doingText({ turn: 4 })).toBe('Working, turn 4');
    expect(doingText({ turn: null })).toBe('Working');
  });
});

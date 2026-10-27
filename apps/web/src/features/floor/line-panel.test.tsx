// @vitest-environment jsdom
import { cleanup, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { Board, BoardStation, ItemCard } from '../../api/types';
import { LinePanel } from './line-panel';

afterEach(cleanup);

const card = (number: number, state: ItemCard['state']): ItemCard => ({
  id: `item-${number}`,
  ref: `software#${number}`,
  number,
  title: `Change ${number}`,
  type: 'feature',
  station: 'build',
  state,
  class_of_service: 'standard',
  rework_count: 0,
  cost_usd: 0,
  created_at: '2026-10-25T08:00:00Z',
  updated_at: '2026-10-25T08:00:00Z',
  age_seconds: 600,
  agent: null,
  pr_url: null,
  pull_requests: [],
  slot: null,
  turn: null,
  summary: null,
  in_buffer: false,
  rope_held: false,
  touch_count: 0,
});

const station = (over: Partial<BoardStation> & Pick<BoardStation, 'id' | 'kind'>): BoardStation => ({
  agents: [],
  wip_limit: null,
  waiting: 0,
  in_progress: 0,
  items: [],
  busy_ratio: null,
  send_back_ratio: null,
  queue_trend: null,
  oldest_waiting_seconds: null,
  done_today: null,
  lanes: [],
  ...over,
});

/** The Software line as the Floor draws it: build is the constraint, with a buffer of two and one person's item. */
const line: Board = {
  factory: { id: 'factory-2', name: 'software', title: 'Software' },
  stations: [
    station({
      id: 'intake',
      kind: 'queue',
      waiting: 4,
      oldest_waiting_seconds: 3 * 3600,
      queue_trend: 'growing',
      items: [card(1, 'queued'), card(2, 'queued'), card(3, 'queued'), card(4, 'queued')],
    }),
    station({
      id: 'spec',
      kind: 'agent',
      agents: ['architect'],
      wip_limit: 3,
      in_progress: 2,
      busy_ratio: 0.41,
      items: [card(5, 'in_progress'), card(6, 'waiting_human')],
      lanes: [
        { key: 'architect', kind: 'agent', role: 'Architect', model: null, model_label: null, slots: 3, busy: 1 },
      ],
    }),
    station({
      id: 'build',
      kind: 'agent',
      agents: ['engineer'],
      wip_limit: 6,
      waiting: 2,
      in_progress: 6,
      busy_ratio: 0.94,
      queue_trend: 'growing',
      items: [
        card(7, 'ready'),
        card(8, 'ready'),
        card(9, 'in_progress'),
        card(10, 'in_progress'),
        card(11, 'in_progress'),
        card(12, 'in_progress'),
        card(13, 'in_progress'),
        card(14, 'waiting_human'),
      ],
      lanes: [{ key: 'engineer', kind: 'agent', role: 'Engineer', model: null, model_label: null, slots: 6, busy: 5 }],
    }),
    station({
      id: 'review',
      kind: 'agent',
      agents: ['reviewer'],
      wip_limit: 3,
      in_progress: 2,
      send_back_ratio: 0.14,
      items: [card(15, 'in_progress'), card(16, 'waiting_human')],
      lanes: [
        { key: 'reviewer', kind: 'agent', role: 'Reviewer', model: null, model_label: null, slots: 3, busy: 1 },
      ],
    }),
    station({ id: 'done', kind: 'done', done_today: 7 }),
  ],
  assemblies: [],
  constraint: 'build',
  constraint_source: 'configured',
  line: {
    status: 'running',
    andon: null,
    wip: { in_progress: 10, limit: 18 },
    release: { policy: 'drum-buffer-rope', enforced: false, drum: 'build', buffer: 2, in_buffer: 2 },
    expedite: { limit: 1, in_use: 1 },
    budget: null,
    touches: { finished: 0, per_item_mean: 0, per_item_median: 0, touchless_rate: 0 },
  },
};

describe('LinePanel', () => {
  it('draws the line with its legend and the stations in order, from intake to done', () => {
    render(<LinePanel board={line} constraint="build" doneThisWeek={53} />);

    expect(screen.getByRole('heading', { level: 2, name: 'The line, now' })).toBeTruthy();
    const legend = screen.getByRole('group', { name: 'Legend' });
    const stations = within(screen.getByRole('list', { name: 'Stations from intake to done' })).getAllByRole('listitem');
    for (const name of ['Agent working', 'Waiting on you', 'Free']) {
      expect(within(legend).getByText(name)).toBeTruthy();
    }
    const names = stations.map((li) => within(li).getByText(/^(Intake|Spec|Build|Review|Done)$/).firstChild?.textContent);
    expect(names).toEqual(['Intake', 'Spec', 'Build', 'Review', 'Done']);
  });

  it('numbers each station by the items waiting and in progress there', () => {
    render(<LinePanel board={line} constraint="build" doneThisWeek={53} />);

    const build = screen.getByText('Build').closest('li') as HTMLElement;
    expect(within(build).getByText('8')).toBeTruthy();
    const intake = screen.getByText('Intake').closest('li') as HTMLElement;
    expect(within(intake).getByText('4')).toBeTruthy();
  });

  it('says what waits at each station: the queue, the buffer at the drum, and the work in progress', () => {
    render(<LinePanel board={line} constraint="build" doneThisWeek={53} />);

    expect(screen.getByText('4 waiting')).toBeTruthy();
    expect(screen.getByText('Oldest 3 h')).toBeTruthy();
    expect(screen.getByText('2 ready in buffer')).toBeTruthy();
    expect(screen.getAllByText('2 in progress')).toHaveLength(2);
  });

  it('draws each agent slot as working, waiting on you or free, and names the role', () => {
    render(<LinePanel board={line} constraint="build" doneThisWeek={53} />);

    expect(screen.getByRole('img', { name: 'Architect: 1 working, 1 waiting on you, 1 free' })).toBeTruthy();
    expect(screen.getByRole('img', { name: 'Engineer: 5 working, 1 waiting on you, 0 free' })).toBeTruthy();
    expect(screen.getByRole('img', { name: 'Reviewer: 1 working, 1 waiting on you, 1 free' })).toBeTruthy();
  });

  it('gives the busy share, or the send-back share where the station has no busy share', () => {
    render(<LinePanel board={line} constraint="build" doneThisWeek={53} />);

    expect(screen.getByText('Busy 41%')).toBeTruthy();
    expect(screen.getByText('Busy 94%')).toBeTruthy();
    expect(screen.getByText('Sends back 14%')).toBeTruthy();
  });

  it('marks the constraint with a Constraint tag and gives Done its counts', () => {
    render(<LinePanel board={line} constraint="build" doneThisWeek={53} />);

    const build = screen.getByText('Build').closest('li') as HTMLElement;
    expect(within(build).getByText('Constraint')).toBeTruthy();
    expect(within(screen.getByText('Done').closest('li') as HTMLElement).getByText('7 done today')).toBeTruthy();
    expect(screen.getByText('53 this week')).toBeTruthy();
  });

  it('footer: in progress, release to the drum, the expedite lane and the andon state', () => {
    const { container } = render(<LinePanel board={line} constraint="build" doneThisWeek={53} />);

    const footer = container.querySelector('footer')?.textContent ?? '';
    expect(footer).toContain('In progress 10 of 18, ready 2');
    expect(footer).toContain('New work enters when Build has room');
    expect(footer).toContain('Expedite lane 1 of 1');
    expect(footer).toContain('Andon clear');
  });

  it('leaves out release and expedite when the policy has no drum or no lane is set', () => {
    render(
      <LinePanel
        board={{
          ...line,
          line: { ...line.line, release: { ...line.line.release, policy: 'push', drum: null }, expedite: null },
        }}
        constraint={null}
        doneThisWeek={null}
      />,
    );

    expect(screen.queryByText(/New work enters/)).toBeNull();
    expect(screen.queryByText(/Expedite lane/)).toBeNull();
  });

  it('shows an empty line when the factory has no stations yet', () => {
    render(<LinePanel board={{ ...line, stations: [] }} constraint={null} doneThisWeek={null} />);

    expect(screen.getByText('This factory has no stations yet.')).toBeTruthy();
  });
});

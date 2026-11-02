// @vitest-environment jsdom
import { cleanup, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { Factory, ItemCard, ItemWaiting, OrgSummary, PartsSummary } from '../../api/types';
import { moveShortcut } from '../../shared/move-menu';
import { renderFactoryPage } from '../../test/route';
import { BoardCard } from './board-card';

afterEach(cleanup);

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'sre',
  title: 'SRE',
  work_types: ['feature'],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'approver',
  can: ['item:operate', 'item:decide'],
};

const card = (over: Partial<ItemCard>): ItemCard => ({
  id: 'item-21',
  ref: 'sre#21',
  number: 21,
  title: 'Adjust the export handler',
  type: 'task',
  station: 'build',
  state: 'queued',
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
  ...over,
});

const shared = (refs: string[]): ItemWaiting => ({
  kind: 'files',
  items: refs,
  files: ['internal/api/gen/export/handlers.go'],
  needs: refs.map((ref) => ({ ref, title: 'Move the export', paths: ['internal/api/gen/**'] })),
});

const renderCard = (item: ItemCard) =>
  renderFactoryPage({ page: () => <BoardCard item={item} />, path: 'board', org, factory });

const renderMovable = (item: ItemCard, assemblyParts?: PartsSummary) =>
  renderFactoryPage({
    page: () => <BoardCard item={item} onMove={() => {}} assemblyParts={assemblyParts} />,
    path: 'board',
    org,
    factory,
  });

describe('BoardCard wait line', () => {
  it('shows one wait line for one needed item, with no path or glob on the card', async () => {
    renderCard(card({ waiting: shared(['sre#3']) }));

    const article = (await screen.findByRole('link', { name: 'Adjust the export handler' })).closest(
      'article',
    ) as HTMLElement;
    expect(within(article).getByText('Waits for sre#3')).toBeTruthy();
    expect(article.textContent).not.toMatch(/\*\*|internal\/|same files/);
    expect(article.querySelector('.text-rework')).toBeNull();
    expect(within(article).queryByRole('link', { name: 'sre#3' })).toBeNull();
  });

  it('shows one wait line with the count of the others for several needed items', async () => {
    renderCard(card({ waiting: shared(['sre#3', 'sre#4', 'sre#5']) }));

    const article = (await screen.findByRole('link', { name: 'Adjust the export handler' })).closest(
      'article',
    ) as HTMLElement;
    expect(within(article).getByText('Waits for sre#3 +2')).toBeTruthy();
    expect(article.querySelectorAll('.truncate')).toHaveLength(1);
  });

  it('shows no wait line for an item that waits on nothing', async () => {
    renderCard(card({ waiting: null }));

    const article = (await screen.findByRole('link', { name: 'Adjust the export handler' })).closest(
      'article',
    ) as HTMLElement;
    expect(within(article).queryByText(/^Waits for/)).toBeNull();
  });
});

describe('BoardCard as the screen draws it', () => {
  const article = async () =>
    (await screen.findByRole('link', { name: 'Adjust the export handler' })).closest('article') as HTMLElement;

  it('shows the mono number, plain type and lane text, the age inline, the agent with its cost, and its state chip', async () => {
    renderMovable(
      card({
        type: 'feature',
        lane: 'backend',
        age_seconds: 600,
        state: 'waiting_human',
        agent: 'builder',
        cost_usd: 0.7,
        slot: 1,
        turn: 2,
        touch_count: 2,
      }),
    );

    const card_ = await article();
    const number = within(card_).getByText('#21');
    expect(number.classList).toContain('font-mono');
    expect(card_.textContent).not.toContain('sre#21');
    // Type, lane and age are plain text on the one meta row with the number, not bordered chips.
    const type = within(card_).getByText('feature');
    const lane = within(card_).getByText('backend');
    const age = within(card_).getByText('10 min');
    expect(type.parentElement).toBe(number.parentElement);
    expect(lane.parentElement).toBe(number.parentElement);
    expect(age.parentElement).toBe(number.parentElement);
    expect(type.classList).not.toContain('border');
    expect(lane.classList).not.toContain('border');
    expect(within(card_).queryByText('Feature')).toBeNull();
    expect(within(card_).getByText('builder')).toBeTruthy();
    expect(within(card_).getByText('$0.70')).toBeTruthy();
    expect(within(card_).getByText('Needs your OK')).toBeTruthy();
    expect(card_.textContent).not.toMatch(/copy/i);
    // 12 px padding, as the design's card.
    expect(card_.classList).toContain('p-3');
    // Touches are muted text, not red.
    const touches = within(card_).getByText('2 touches');
    expect(touches.classList).toContain('text-muted');
    expect(touches.className).not.toMatch(/you|rework|rose|red/);
    // The agent's mark is an outlined-tone avatar with no bold weight.
    const mark = within(card_).getByText('B');
    expect(mark.className).not.toMatch(/font-bold/);
    expect(mark.classList).toContain('font-semibold');
    expect(mark.classList).not.toContain('bg-run');
  });

  it('shows its Move button only on hover or focus on a desktop card, not as an arrow on every card', async () => {
    renderMovable(card({ state: 'queued' }));

    const button = within(await article()).getByRole('button', { name: /move/i });
    expect(button.className).toMatch(/md:opacity-0/);
    expect(button.className).toMatch(/md:group-hover:opacity-100/);
    expect(button.className).toMatch(/md:focus-visible:opacity-100/);
  });

  it('keeps the Move button in the status row on a phone and reserves room for it from md', async () => {
    renderMovable(card({ state: 'queued' }));

    const button = within(await article()).getByRole('button', { name: /move/i });
    expect(button.className).not.toMatch(/(^|\s)absolute(\s|$)/);
    expect(button.className).toMatch(/md:absolute/);
    expect(button.parentElement?.className).toMatch(/md:pr-10/);
  });

  it('shows the assembly line: the part, one pip per part with the merged ones filled', async () => {
    renderMovable(
      card({
        assembly: { id: 'item-50', ref: 'sre#50', title: 'PWA', part: 2, parts: 3 },
      }),
      { total: 3, merged: 1, in_progress: 1, waiting: 0, stations: [] },
    );

    const card_ = await article();
    expect(within(card_).getByRole('link', { name: 'Part 2 of 3 of PWA' })).toBeTruthy();
    expect(card_.querySelectorAll('i.bg-ink')).toHaveLength(1);
    expect(card_.querySelectorAll('i.bg-rule')).toHaveLength(2);
  });

  it('keeps the Move button and the M shortcut on the card', async () => {
    renderMovable(card({ state: 'queued' }));

    const card_ = await article();
    expect(within(card_).getByRole('button', { name: /move/i })).toBeTruthy();
    expect(
      within(card_).getByRole('link', { name: 'Adjust the export handler' }).getAttribute('aria-keyshortcuts'),
    ).toBe(moveShortcut);
  });
});

// @vitest-environment jsdom
import { cleanup, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { Factory, ItemCard, ItemWaiting, OrgSummary } from '../../api/types';
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

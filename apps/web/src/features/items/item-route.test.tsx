// @vitest-environment jsdom
import { cleanup, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { keys } from '../../api/queries';
import type { AssemblyParts, Factory, Item, OrgSummary } from '../../api/types';
import { renderFactoryPage, renderItemPage } from '../../test/route';
import { TooltipProvider } from '../../ui/tooltip';
import { ItemRoute } from './item-route';

afterEach(cleanup);

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'goal',
  title: 'Goal',
  work_types: ['feature'],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'approver',
  can: [],
};

function item(state: Item['state']): Item {
  return {
    id: 'item-40',
    ref: 'goal#40',
    factory_id: 'factory-1',
    number: 40,
    type: 'feature',
    title: 'Ship the thing',
    body: 'A short brief.',
    station: 'done',
    state,
    class_of_service: 'standard',
    priority: 0,
    rework_count: 0,
    cost_usd: 0.5,
    outputs: {},
    created_at: '2026-10-20T00:00:00Z',
    updated_at: '2026-10-25T00:00:00Z',
  } as Item;
}

const none: AssemblyParts = { done: 0, total: 0, lead_time_seconds: 0, parts: [] };
const two: AssemblyParts = {
  done: 2,
  total: 2,
  lead_time_seconds: 86400,
  parts: [1, 2].map((n) => ({
    id: `part-${n}`,
    ref: `goal#${40 + n}`,
    number: 40 + n,
    index: n,
    key: `p${n}`,
    title: `Part ${n} title`,
    paths: [],
    station: 'build',
    state: 'done',
    needs: [],
    waiting_on: [],
    wave: 0,
    cost_usd: 1,
    pr_url: null,
    agent: null,
    created_at: '2026-10-20T00:00:00Z',
    done_at: '2026-10-22T00:00:00Z',
  })) as AssemblyParts['parts'],
};

describe('ItemRoute', () => {
  it('keeps the assembly page for a done assembly: Progress, Parts and Plan', async () => {
    renderItemPage({ page: () => <ItemRoute />, org, factory, item: item('done'), parts: two });

    expect(await screen.findByRole('region', { name: 'Progress' })).toBeTruthy();
    expect(screen.getByRole('table', { name: /Parts of goal#40/ })).toBeTruthy();
    expect(screen.getByRole('region', { name: 'Plan' })).toBeTruthy();
  });

  it('shows the item page for an item with no parts', async () => {
    renderFactoryPage({
      page: () => (
        <TooltipProvider>
          <ItemRoute />
        </TooltipProvider>
      ),
      path: 'items/$ref',
      location: 'items/40',
      org,
      factory,
      seed: [
        [keys.item(org.id, 'goal#40'), item('done')],
        [keys.events(org.id, 'goal#40'), []],
        [keys.itemRuns(org.id, 'goal#40'), { runs: [] }],
        [keys.parts(org.id, 'goal#40'), none],
      ],
    });

    await screen.findByRole('heading', { level: 1, name: 'Ship the thing' });
    expect(screen.queryByRole('region', { name: 'Progress' })).toBeNull();
    expect(screen.queryByRole('table', { name: /Parts of/ })).toBeNull();
  });
});

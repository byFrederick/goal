// @vitest-environment jsdom
import { cleanup, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { Factory, OrgSummary } from '../api/types';
import { renderFactoryPage } from '../test/route';
import { CameFrom } from './origin';

afterEach(cleanup);

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'Software',
  work_types: [],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'owner',
} as unknown as Factory;

async function text(item: Parameters<typeof CameFrom>[0]['item']) {
  const { container } = renderFactoryPage({
    page: () => (
      <p data-testid="came">
        <CameFrom item={item} />
      </p>
    ),
    path: 'items/$ref',
    location: 'items/1',
    org,
    factory,
  });
  await screen.findByTestId('came');
  return container.textContent;
}

const followUp = (id: string, from: string, landed: string) => ({
  id,
  from_item: from,
  from_item_id: `${id}-f`,
  agent: 'backend-engineer',
  station: 'build',
  landed_item: landed,
  landed_item_id: `${id}-l`,
});

describe('CameFrom', () => {
  it('names the agent, the source and the landed item of each follow-up', async () => {
    const t = await text({
      origin: 'follow-up',
      came_from: { follow_ups: [followUp('a', 'Software#519', 'Software#513'), followUp('b', 'Other#2', 'Other#1')] },
    });
    expect(t).toContain('A follow-up by backend-engineer on Software#519, filed when Software#513 landed');
    expect(t).toContain('A follow-up by backend-engineer on Other#2, filed when Other#1 landed');
    expect(screen.getByRole('link', { name: 'Other#2' })).toBeTruthy();
  });

  it('falls back when the sources are unknown', async () => {
    expect(await text({ origin: 'follow-up', came_from: { follow_ups: [] } })).toBe('A follow-up');
    cleanup();
    expect(await text({ origin: 'follow-up', from_item: 'Software#4' })).toBe('Follow-up from Software#4');
  });

  it('names a check and its key', async () => {
    const t = await text({
      origin: 'trigger',
      came_from: { trigger: { name: 'New incidents', kind: 'check', key: 'Q3T8' } },
    });
    expect(t).toBe('The check New incidents, Q3T8');
    cleanup();
    expect(await text({ origin: 'trigger', came_from: { trigger: { name: 'New incidents', kind: 'check' } } })).toBe(
      'The check New incidents',
    );
  });

  it('names a schedule or a webhook trigger alone, and a bare trigger generically', async () => {
    expect(await text({ origin: 'trigger', came_from: { trigger: { name: 'Nightly', kind: 'schedule' } } })).toBe(
      'Nightly',
    );
    cleanup();
    expect(await text({ origin: 'trigger', came_from: { trigger: { name: 'Issues', kind: 'webhook' } } })).toBe(
      'Issues',
    );
    cleanup();
    expect(await text({ origin: 'trigger', came_from: { trigger: { name: 'Gone' } } })).toBe('Gone');
    cleanup();
    expect(await text({ origin: 'trigger' })).toBe('Filed by a trigger');
  });
});

// @vitest-environment jsdom
import { cleanup, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { Factory, ItemWaiting, OrgSummary } from '../api/types';
import { renderFactoryPage } from '../test/route';
import { WaitingFor } from './waiting-for';

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
  can: ['item:operate', 'item:decide'],
};

const files: ItemWaiting = {
  kind: 'files',
  items: ['goal#13'],
  files: ['internal/knowledge/sync.go'],
  needs: [{ ref: 'goal#13', title: 'Spec the knowledge sync retry', paths: ['internal/knowledge/**'] }],
};

describe('WaitingFor', () => {
  it('reads the same-files line and links its ref to the needed item page', async () => {
    renderFactoryPage({ page: () => <WaitingFor waiting={files} />, path: 'board', org, factory });

    const link = await screen.findByRole('link', { name: 'goal#13' });
    expect(link.getAttribute('href')).toBe('/o/acme/f/goal/items/13');
    expect((await screen.findByRole('listitem')).getAttribute('title')).toBe(
      'waits for the same files as goal#13 (internal/knowledge/)',
    );
  });

  it('reads the moves line for kind after, with the needed title when it names no paths', async () => {
    const after: ItemWaiting = {
      kind: 'after',
      items: ['goal#16'],
      files: [],
      needs: [{ ref: 'goal#16', title: 'goald exits when the bucket has no factories', paths: [] }],
    };
    renderFactoryPage({ page: () => <WaitingFor waiting={after} />, path: 'board', org, factory });

    expect((await screen.findByRole('link', { name: 'goal#16' })).getAttribute('href')).toBe('/o/acme/f/goal/items/16');
    expect((await screen.findByRole('listitem')).getAttribute('title')).toBe(
      'waits for goal#16 (moves goald exits when the bucket has no factories)',
    );
  });

  it('renders nothing for an item that does not wait', async () => {
    renderFactoryPage({
      page: () => (
        <>
          <p>Nothing waits</p>
          <WaitingFor waiting={null} />
        </>
      ),
      path: 'board',
      org,
      factory,
    });
    await screen.findByText('Nothing waits');
    expect(screen.queryByRole('list')).toBeNull();
  });
});

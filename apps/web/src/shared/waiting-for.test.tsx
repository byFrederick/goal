// @vitest-environment jsdom
import { cleanup, screen, within } from '@testing-library/react';
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
  it('heads a Waiting section with the need, its paths and the shared files, the ref linked to its item page', async () => {
    renderFactoryPage({ page: () => <WaitingFor waiting={files} />, path: 'board', org, factory });

    const section = await screen.findByRole('region', { name: 'Waiting' });
    expect(within(section).getByText(/Same files as/)).toBeTruthy();
    const link = within(section).getByRole('link', { name: 'goal#13' });
    expect(link.getAttribute('href')).toBe('/o/acme/f/goal/items/13');
    expect(within(section).getByText(/Spec the knowledge sync retry/)).toBeTruthy();
    expect(within(section).getByText('internal/knowledge/**')).toBeTruthy();
    expect(within(section).getByText('Shared files: internal/knowledge/sync.go')).toBeTruthy();
  });

  it('lists every open need of a wait for another item, each with its own link', async () => {
    const after: ItemWaiting = {
      kind: 'after',
      items: ['goal#16', 'goal#17'],
      files: [],
      needs: [
        { ref: 'goal#16', title: 'goald exits when the bucket has no factories', paths: [] },
        { ref: 'goal#17', title: 'Move the bucket', paths: ['deploy/terraform/**'] },
      ],
    };
    renderFactoryPage({ page: () => <WaitingFor waiting={after} />, path: 'board', org, factory });

    const section = await screen.findByRole('region', { name: 'Waiting' });
    expect(within(section).getAllByRole('listitem')).toHaveLength(2);
    expect(within(section).getByRole('link', { name: 'goal#16' }).getAttribute('href')).toBe('/o/acme/f/goal/items/16');
    expect(within(section).getByRole('link', { name: 'goal#17' }).getAttribute('href')).toBe('/o/acme/f/goal/items/17');
    expect(within(section).queryByText(/Shared files/)).toBeNull();
  });

  it('words a turn wait with its branch, a linked ref and an ⓘ, and no ⓘ for other waits', async () => {
    const turn: ItemWaiting = {
      kind: 'turn',
      items: ['goal#16'],
      files: [],
      target: 'goal/a7-x',
      needs: [{ ref: 'goal#16', title: 'Retry the sync', paths: [] }],
    };
    renderFactoryPage({ page: () => <WaitingFor waiting={turn} />, path: 'board', org, factory });

    const section = await screen.findByRole('region', { name: 'Waiting' });
    expect(section.textContent).toContain('Waiting for goal#16 to land on goal/a7-x Retry the sync');
    expect(within(section).getByRole('link', { name: 'goal#16' }).getAttribute('href')).toBe('/o/acme/f/goal/items/16');
    const tip = within(section).getByRole('button', { name: 'About Waiting' });
    expect(tip.getAttribute('aria-describedby')).toBeTruthy();
    expect(
      within(section).getByText(
        /This station merges one item into a branch at a time, so each pull request is tested against the latest branch\. This item starts when the one ahead is merged or sent back\./,
      ),
    ).toBeTruthy();
  });

  it('says the default branch when a turn wait has no target', async () => {
    const turn: ItemWaiting = {
      kind: 'turn',
      items: ['goal#16'],
      files: [],
      target: null,
      needs: [{ ref: 'goal#16', title: 'Retry the sync', paths: [] }],
    };
    renderFactoryPage({ page: () => <WaitingFor waiting={turn} />, path: 'board', org, factory });
    const section = await screen.findByRole('region', { name: 'Waiting' });
    expect(section.textContent).toContain('to land on the default branch');
  });

  it('shows no ⓘ for a files wait', async () => {
    renderFactoryPage({ page: () => <WaitingFor waiting={files} />, path: 'board', org, factory });
    await screen.findByRole('region', { name: 'Waiting' });
    expect(screen.queryByRole('button', { name: 'About Waiting' })).toBeNull();
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
    expect(screen.queryByRole('region', { name: 'Waiting' })).toBeNull();
  });
});

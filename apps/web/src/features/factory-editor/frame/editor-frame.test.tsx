// @vitest-environment jsdom
import { cleanup, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { defKeys } from '../../../api/queries';
import type { Factory, OrgSummary } from '../../../api/types';
import { renderFactoryPage } from '../../../test/route';
import { DraftSessionProvider } from '../draft/use-draft';
import { DefinitionFrame } from './editor-frame';

afterEach(cleanup);

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: 'owner' };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'sre',
  title: 'SRE',
  work_types: [],
  stations: [],
  current: {
    id: 'version-21',
    number: 21,
    published_at: '2026-10-01T00:00:00Z',
    published_by: { kind: 'user', id: 'u-1', name: 'Freddy Cruz' },
  },
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'editor',
  can: ['definition:edit'],
};

/** Renders the Definition frame with the top bar above it, as the caller's `can` allows. */
async function renderFrame(can: string[]) {
  const subject = { ...factory, can };
  const edits = can.includes('definition:edit');
  renderFactoryPage({
    page: () => (
      <DraftSessionProvider org={org.id} factory={subject.id} readOnly={!edits}>
        <DefinitionFrame section="basics" yaml={false}>
          <p>section</p>
        </DefinitionFrame>
      </DraftSessionProvider>
    ),
    path: 'definition/basics',
    org,
    factory: subject,
    seed: [[defKeys.factory(org.id, subject.id), subject]],
    topBar: true,
  });
  await screen.findByText('section');
  return within(screen.getAllByRole('banner')[0] as HTMLElement);
}

describe('the Definition frame’s actions in the top bar', () => {
  it('shows Form | YAML, Versions, Review changes and Publish, Publish last', async () => {
    const bar = await renderFrame(['definition:edit']);

    expect(bar.getByRole('group', { name: 'Editor view' })).toBeTruthy();
    expect(bar.getByRole('link', { name: 'Versions' })).toBeTruthy();
    expect(bar.getByRole('link', { name: 'Review changes' })).toBeTruthy();
    const publish = bar.getByRole('button', { name: 'Publish v22' });
    const last = bar.getAllByRole('button', { hidden: false }).at(-1);
    expect(last).toBe(publish);
    expect(screen.getAllByRole('button', { name: 'Publish v22' })).toHaveLength(1);
  });

  it('keeps Publish disabled, with its reason, while the draft has no changes', async () => {
    const bar = await renderFrame(['definition:edit']);

    const publish = bar.getByRole('button', { name: 'Publish v22' }) as HTMLButtonElement;
    expect(publish.disabled).toBe(true);
    const reason = document.getElementById(publish.getAttribute('aria-describedby') ?? '');
    expect(reason?.textContent).toBe('No changes yet');
  });

  it('shows a viewer only Versions: no Review changes and no Publish', async () => {
    const bar = await renderFrame([]);

    expect(bar.getByRole('link', { name: 'Versions' })).toBeTruthy();
    expect(screen.queryByRole('link', { name: 'Review changes' })).toBeNull();
    expect(screen.queryByRole('button', { name: /^Publish/ })).toBeNull();
  });
});

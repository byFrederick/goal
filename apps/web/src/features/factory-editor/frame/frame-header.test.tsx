// @vitest-environment jsdom
import { cleanup, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { factoryQuery, versionModelQuery } from '../../../api/queries';
import type { Factory, OrgSummary } from '../../../api/types';
import { renderFactoryPage } from '../../../test/route';
import { DraftSessionProvider } from '../draft/use-draft';
import { DefinitionFrame } from './editor-frame';
import { WizardHeader } from './wizard-frame';

vi.mock('../../../app/top-bar', async (original) => ({
  ...(await original<typeof import('../../../app/top-bar')>()),
  TopBarSlot: ({ back }: { back?: React.ReactNode }) => back ?? null,
}));

afterEach(() => {
  cleanup();
  delete (window as { matchMedia?: unknown }).matchMedia;
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: 'owner' };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'goal',
  title: 'Goal',
  work_types: [],
  stations: [],
  current: { number: 8 },
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'viewer',
  can: [],
} as unknown as Factory;

function onPhone() {
  window.matchMedia = ((query: string) =>
    ({
      matches: query === '(max-width: 767px)',
      media: query,
      addEventListener: () => {},
      removeEventListener: () => {},
    }) as unknown as MediaQueryList) as typeof window.matchMedia;
}

describe('the Definition header', () => {
  it('shows the heading and a "v8 running" chip, with no breadcrumb, factory link, badge or "Live:"', async () => {
    renderFactoryPage({
      page: () => (
        <DraftSessionProvider org={org.id} factory={factory.id} readOnly>
          <DefinitionFrame section="line" yaml={false}>
            <p>Section</p>
          </DefinitionFrame>
        </DraftSessionProvider>
      ),
      path: 'definition/line',
      org,
      factory,
      seed: [
        [factoryQuery(org.id, factory.id).queryKey, factory],
        [
          versionModelQuery(org.id, factory.id, 'current').queryKey,
          {
            revision: 1,
            model: {
              basics: { id: 'f1', name: 'goal', work_types: [] },
              line: { stations: [] },
              agents: { agents: [], rules: [] },
              connections: {},
              review: {},
            },
            etags: { basics: 'b', line: 'l', agents: 'a', connections: 'c', review: 'r' },
            edited: {},
            ranges: {},
          },
        ],
      ],
    });
    await screen.findByRole('heading', { level: 1, name: 'Definition' });
    expect(screen.getByText('v8 running')).toBeTruthy();
    expect(screen.queryByRole('navigation', { name: 'Breadcrumb' })).toBeNull();
    expect(screen.queryByText(/Live:/)).toBeNull();
    expect(screen.queryByRole('link', { name: 'goal' })).toBeNull();
    expect(screen.queryByRole('link', { name: /^Back to/ })).toBeNull();
  });
});

describe('the new-factory wizard header', () => {
  it('has no breadcrumb, and a phone gets Back to Factories in the top bar', async () => {
    onPhone();
    renderFactoryPage({
      page: () => <WizardHeader title="New factory" description="Start" actions={null} />,
      path: 'definition/line',
      org,
      factory,
    });
    await screen.findByRole('heading', { level: 1, name: 'New factory' });
    expect(screen.queryByRole('navigation', { name: 'Breadcrumb' })).toBeNull();
    expect(screen.getByRole('link', { name: 'Back to Factories' }).getAttribute('href')).toBe('/o/acme');
  });
});

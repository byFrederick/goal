// @vitest-environment jsdom
import { cleanup, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { ConstraintCandidate, Factory, OrgSummary } from '../../api/types';
import { renderFactoryPage } from '../../test/route';
import { WhatIf } from './waits-card';

afterEach(cleanup);

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'software',
  title: 'Software',
  work_types: ['task'],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'owner',
  can: ['definition:edit'],
} as unknown as Factory;

function candidate(saves: number, spend: number, slots = 2): ConstraintCandidate {
  return {
    station: 'integrate',
    slots,
    agents: [],
    busy_ratio: 0.9,
    avg_waiting: 2,
    estimate: { extra_slot_saves_hours: saves },
    spend_per_slot_day_usd: spend,
  } as unknown as ConstraintCandidate;
}

function show(c: ConstraintCandidate, p85: number | null, wipLimit: number | null) {
  renderFactoryPage({
    page: () => <WhatIf candidate={c} name="integrate" factory={factory} leadTimeP85={p85} wipLimit={wipLimit} />,
    path: '/',
    org,
    factory,
  });
}

describe('WhatIf', () => {
  it('renders nothing when the agent saves 0.3 h on a 3 h lead time', async () => {
    show(candidate(0.3, 5), 3, null);
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByText(/^With a/)).toBeNull();
    expect(screen.queryByRole('button', { name: /^Add an? /u })).toBeNull();
  });

  it('renders nothing when the saving is under 10% of a long lead time', async () => {
    show(candidate(1.5, 5), 30, null);
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByText(/^With a/)).toBeNull();
  });

  it('says Lead time 3 h to 1 h for a saving of 2 h', async () => {
    show(candidate(2, 5), 3, null);
    expect(await screen.findByText('Lead time 3 h to 1 h')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Add an integrate agent' })).toBeTruthy();
  });

  it('says Under $1 more a day for a spend of 0.4, never $0', async () => {
    show(candidate(2, 0.4), 3, null);
    expect(await screen.findByText('Under $1 more a day')).toBeTruthy();
    expect(screen.queryByText(/\$0/)).toBeNull();
  });

  it('keeps About $N more a day from $1', async () => {
    show(candidate(2, 12), 3, null);
    expect(await screen.findByText('About $12 more a day')).toBeTruthy();
  });

  it('renders nothing when the WIP limit already caps the slots', async () => {
    show(candidate(2, 5, 3), 3, 3);
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByText(/^With a/)).toBeNull();
  });

  it('still renders when the WIP limit leaves room for a slot', async () => {
    show(candidate(2, 5, 3), 3, 4);
    expect(await screen.findByText('With a 4th integrate agent')).toBeTruthy();
  });
});

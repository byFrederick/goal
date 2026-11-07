// @vitest-environment jsdom
import { cleanup, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { weeklyQuery } from '../../api/queries';
import type { Factory, OrgSummary, WeeklyThroughput } from '../../api/types';
import { renderFactoryPage } from '../../test/route';
import { fromFirstWeek, WeeklyCard } from './weekly-card';

afterEach(cleanup);

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'support',
  title: 'Support replies',
  work_types: [],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'approver',
  can: [],
};
const range = { from: '2026-08-16', to: '2026-11-08' };
const weeks: WeeklyThroughput = {
  to: '2026-11-08',
  type: null,
  targets: { throughput_per_day: 5, lead_time_hours: null, expedite_lead_time_minutes: null },
  weeks: [
    { from: '2026-10-26', to: '2026-11-01', done: 14, per_day: 2 },
    { from: '2026-11-02', to: '2026-11-08', done: 7, per_day: 1 },
  ],
};

function renderCard(over: { merges?: boolean; subject?: Factory; data?: WeeklyThroughput } = {}) {
  const subject = over.subject ?? factory;
  return renderFactoryPage({
    page: () => <WeeklyCard range={range} merges={over.merges ?? false} />,
    path: '/',
    org,
    factory: subject,
    seed: [[weeklyQuery(org.id, subject.id, range).queryKey, over.data ?? weeks]],
  });
}

describe('WeeklyCard', () => {
  it('says merged on a line with code', async () => {
    renderCard({ merges: true });

    const card = await screen.findByRole('region', { name: 'Throughput by week' });
    expect(within(card).getByText('Items merged a day')).toBeTruthy();
    expect(within(card).getByRole('img', { name: /Items merged a day by week/ })).toBeTruthy();
    expect(card.textContent).toContain('Merged');
    expect(card.textContent).toContain('7 merged');
    expect(card.textContent).not.toMatch(/\bdone\b/i);
  });

  it('says done on a line without code', async () => {
    renderCard({ merges: false });

    const card = await screen.findByRole('region', { name: 'Throughput by week' });
    expect(within(card).getByText('Items done a day')).toBeTruthy();
    expect(within(card).getByRole('img', { name: /Items done a day by week/ })).toBeTruthy();
    expect(card.textContent).toContain('Done');
    expect(card.textContent).toContain('7 done');
    expect(card.textContent).not.toMatch(/merged/i);
  });

  it('starts at the factory first week', async () => {
    renderCard({ subject: { ...factory, created_at: '2026-11-07T09:00:00Z' } });

    const card = await screen.findByRole('region', { name: 'Throughput by week' });
    expect(card.textContent).not.toMatch(/Oct/);
    expect(card.textContent).toContain('Nov');
  });

  it('shows its empty state when no week reaches the factory', async () => {
    renderCard({ subject: { ...factory, created_at: '2026-12-01T00:00:00Z' } });

    const card = await screen.findByRole('region', { name: 'Throughput by week' });
    expect(within(card).getByText('No finished items in these weeks yet.')).toBeTruthy();
  });
});

describe('fromFirstWeek', () => {
  it('keeps the week that holds the creation day and drops the ones before', () => {
    const kept = fromFirstWeek(weeks, '2026-11-01T23:00:00Z');
    expect(kept.weeks.map((w) => w.from)).toEqual(['2026-10-26', '2026-11-02']);
    expect(fromFirstWeek(weeks, '2026-11-02T00:00:00Z').weeks.map((w) => w.from)).toEqual(['2026-11-02']);
  });
});

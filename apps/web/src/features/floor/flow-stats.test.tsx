// @vitest-environment jsdom
import { cleanup, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { Metrics, MetricsSummary } from '../../api/types';
import { FlowStats } from './flow-stats';

afterEach(cleanup);

const before: MetricsSummary = {
  from: '2026-10-13',
  to: '2026-10-19',
  items_done: 0,
  throughput_per_day: 0,
  lead_time_hours: null,
  assembly_lead_time_hours: null,
  rework_rate: 0,
};

const base: Metrics = {
  window: '7d',
  from: '2026-10-20',
  to: '2026-10-26',
  type: null,
  throughput: [],
  throughput_per_day: 0,
  lead_time_hours: null,
  assembly_lead_time_hours: null,
  wip: 0,
  rework_rate: 0,
  spend_usd: 0,
  items_done: 0,
  previous: before,
  targets: { throughput_per_day: null, lead_time_hours: null, expedite_lead_time_minutes: null },
  long_waits: null,
};

function throughputMeta() {
  const strip = screen.getByRole('region', { name: 'Flow against targets' });
  return (within(strip).getByText('Throughput').parentElement as HTMLElement).lastElementChild as HTMLElement;
}

describe('FlowStats', () => {
  it('shows no Throughput delta when neither range has a finished item, and keeps No target set', () => {
    render(<FlowStats metrics={base} />);

    const meta = throughputMeta();
    expect(meta.querySelector('.text-rework-ink, .text-ok-ink')).toBeNull();
    expect(within(meta).getByText('No target set')).toBeTruthy();
  });

  it('draws a zero delta neutral and keeps No target set beside it', () => {
    const metrics = {
      ...base,
      items_done: 4,
      throughput_per_day: 2,
      previous: { ...before, items_done: 4, throughput_per_day: 2 },
    };
    render(<FlowStats metrics={metrics} />);

    const meta = throughputMeta();
    const delta = within(meta).getByText('0');
    expect(delta.className).toContain('text-muted');
    expect(delta.className).not.toContain('text-ok-ink');
    expect(delta.className).not.toContain('text-rework-ink');
    expect(within(meta).getByText('No target set')).toBeTruthy();
  });

  it('still draws a drop in rework-ink', () => {
    const metrics = {
      ...base,
      items_done: 4,
      throughput_per_day: 1,
      previous: { ...before, items_done: 4, throughput_per_day: 2 },
    };
    render(<FlowStats metrics={metrics} />);

    expect(within(throughputMeta()).getByText('−1').className).toContain('text-rework-ink');
  });
});

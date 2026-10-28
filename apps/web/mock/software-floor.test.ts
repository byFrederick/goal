import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import type { ConstraintReport, Metrics, TouchMetrics } from '@goal/api-client';
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import { createMockApi, type MockApi } from './api.ts';

/**
 * The software factory's Floor figures the design picture shows (82% hands-off, a 31 h p85, about $18 a day
 * for one more build agent), read from the mock's own history and constraint. The saving of one more slot
 * (31 h to 25 h) is not pictured here: WaitSaved caps it at the mean wait, which the two ready items in the
 * build buffer keep near half an hour.
 */
describe('the mock software Floor', () => {
  let api: MockApi;
  let server: Server;
  let base = '';

  beforeAll(async () => {
    // The seeded history stops at the clock, so the figures depend on the time of day. Pin it.
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date('2026-10-28T23:30:00Z'));
    api = createMockApi();
    server = createServer((req, res) =>
      api.handle(req, res, () => {
        res.statusCode = 404;
        res.end();
      }),
    );
    await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
    base = `http://127.0.0.1:${(server.address() as AddressInfo).port}/v1/orgs/acme/factories/software`;
  });

  afterAll(async () => {
    vi.useRealTimers();
    api.close();
    await new Promise((resolve) => server.close(resolve));
  });

  const get = async <T>(path: string) => (await fetch(`${base}${path}`)).json() as Promise<T>;
  const sevenDays = () => {
    const day = (ms: number) => new Date(ms).toISOString().slice(0, 10);
    const now = Date.now();
    return `from=${day(now - 6 * 86_400_000)}&to=${day(now)}`;
  };

  it('shows 82% hands-off over the week', async () => {
    const touches = await get<TouchMetrics>(`/metrics/touches?${sevenDays()}`);
    expect(Math.round(touches.stats.touchless_rate * 100)).toBe(82);
  });

  it('shows a 31 h lead-time p85 over the week', async () => {
    const metrics = await get<Metrics>(`/metrics?${sevenDays()}`);
    expect(Math.round(metrics.lead_time_hours?.p85 ?? 0)).toBe(31);
  });

  it('has the build station at six slots, and one more agent costs about $18 a day', async () => {
    const report = await get<ConstraintReport>(`/constraint?${sevenDays()}`);
    const build = report.candidates.find((c) => c.station === 'build');
    expect(build?.slots).toBe(6);
    expect(Math.round(build?.spend_per_slot_day_usd ?? 0)).toBe(18);
  });
});

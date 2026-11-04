import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import type { Inbox, InboxEntry } from '@goal/api-client';
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { createMockApi, type MockApi } from './api.ts';

describe('the mock watch_worse inbox entry (proposal 5 of the software factory)', () => {
  let api: MockApi;
  let server: Server;
  let base = '';

  beforeAll(async () => {
    api = createMockApi();
    server = createServer((req, res) =>
      api.handle(req, res, () => {
        res.statusCode = 404;
        res.end();
      }),
    );
    await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
    base = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  });

  afterAll(async () => {
    api.close();
    await new Promise((resolve) => server.close(resolve));
  });

  beforeEach(async () => {
    await fetch(`${base}/__mock/reset`, { method: 'POST' });
  });

  const inbox = async () => ((await (await fetch(`${base}/v1/orgs/acme/inbox`)).json()) as Inbox).entries;
  const worse = async (): Promise<InboxEntry[]> => (await inbox()).filter((e) => e.kind === 'watch_worse');
  const undo = (n: number) =>
    fetch(`${base}/v1/orgs/acme/factories/software/jonah/proposals/${n}/undo`, { method: 'POST' });

  it('lists one entry about no item, with every required field and the watch of proposal 5', async () => {
    const entries = await worse();
    expect(entries).toHaveLength(1);
    const [e] = entries as [InboxEntry];

    expect(e.id).toMatch(/^watch_worse:.+/);
    expect(e.factory.name).toBe('software');
    expect(e.item).toBeNull();
    expect(e.station).toBe(e.watch?.station);
    expect([e.action, e.approval, e.blocked, e.andon, e.rework]).toEqual([null, null, null, null, null]);
    expect(Number.isNaN(Date.parse(e.since))).toBe(false);
    expect(e.watch).toMatchObject({ proposal: 5, verdict: 'worse', metric: 'sent_back_rate', closed_at: null });
  });

  it('sorts after every other kind', async () => {
    const entries = await inbox();
    expect(entries.length).toBeGreaterThan(1);
    expect(entries.at(-1)?.kind).toBe('watch_worse');
  });

  it('is gone once the undo of proposal 5 is written, and back after a reset', async () => {
    expect((await undo(5)).status).toBe(201);
    expect(await worse()).toHaveLength(0);

    await fetch(`${base}/__mock/reset`, { method: 'POST' });
    expect(await worse()).toHaveLength(1);
  });
});

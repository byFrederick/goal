import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import type { Inbox, InboxEntry, Item } from '@goal/api-client';
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { createMockApi, type MockApi } from './api.ts';

describe('the mock tool approvals (ADR 0031)', () => {
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

  const call = async <T>(path: string, method = 'GET', body?: unknown) => {
    const res = await fetch(`${base}${path}`, {
      method,
      headers: { 'Content-Type': 'application/json' },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    });
    const text = await res.text();
    return { status: res.status, body: (text ? JSON.parse(text) : undefined) as T };
  };

  const inbox = '/v1/orgs/acme/inbox';
  const waitingApproval = async (): Promise<InboxEntry> => {
    const { body } = await call<Inbox>(inbox);
    const entry = body.entries.find((e) => e.kind === 'tool_approval');
    if (!entry?.item) throw new Error('the seed has no waiting tool approval');
    return entry;
  };

  it('seeds one waiting tool approval: architect, github, add_issue_comment, on a software item', async () => {
    const entry = await waitingApproval();

    expect(entry.id).toBe(`tool_approval:${entry.item?.id}`);
    expect(entry.item?.ref).toBe('software#151');
    expect(entry.tool_approval).toMatchObject({
      agent: 'architect',
      connection: 'github',
      tool: 'add_issue_comment',
      always_allow_offered: true,
    });
  });

  it('refuses a deny without a note, an approve with a note, and always allow with a deny', async () => {
    const entry = await waitingApproval();
    const path = `/v1/orgs/acme/items/${entry.item?.id}/tool-approval`;

    expect((await call(path, 'POST', { decision: 'deny' })).status).toBe(400);
    expect((await call(path, 'POST', { decision: 'approve', note: 'no' })).status).toBe(400);
    expect((await call(path, 'POST', { decision: 'deny', always_allow: true, note: 'no' })).status).toBe(400);
  });

  it('approves with Always allow, removes the entry and refuses a second decision', async () => {
    const entry = await waitingApproval();
    const path = `/v1/orgs/acme/items/${entry.item?.id}/tool-approval`;

    const first = await call<Item>(path, 'POST', { decision: 'approve', always_allow: true });
    expect(first.status).toBe(200);
    expect(first.body.tool_approval).toBeNull();

    const { body } = await call<Inbox>(inbox);
    expect(body.entries.some((e) => e.kind === 'tool_approval')).toBe(false);

    const second = await call(path, 'POST', { decision: 'approve' });
    expect(second.status).toBe(409);
  });
});

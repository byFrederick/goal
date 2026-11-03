// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../../api/client';
import type { ServerTest, StandingRule } from '../../../api/types';
import { TooltipProvider } from '../../../ui/tooltip';
import { readEnv } from './env-file';
import { McpCard } from './mcp-card';

const holds = vi.hoisted(() => ({ manage: true }));
const api = vi.hoisted(() => ({ list: vi.fn(), revoke: vi.fn() }));
const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));

vi.mock('sonner', () => ({ toast }));
vi.mock('../../../api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../../api/client')>()),
  listStandingRules: api.list,
  revokeStandingRule: api.revoke,
}));
vi.mock('../../../shared/org', () => ({
  useOrg: () => ({ id: 'o1', slug: 'acme', name: 'Acme' }),
  useOrgHolds: () => holds.manage,
}));
vi.mock('../../../shared/factory', () => ({
  useFactoryParams: () => ({ org: 'acme', factory: 'goal' }),
  useFactory: () => ({ id: 'f1', name: 'goal' }),
}));
vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children?: ReactNode }) => <a href="/x">{children}</a>,
}));

const file = `apiVersion: goal/v1
kind: Environment
spec:
  mcp:
    linear:
      url: https://mcp.linear.app/mcp
    sentry:
      url: https://mcp.sentry.dev/mcp
`;

const linear: ServerTest = {
  server: 'linear',
  tested: true,
  ok: true,
  tools: ['list_issues', 'create_comment', 'delete_issue'],
};

function rule(over: Partial<StandingRule>): StandingRule {
  return {
    id: 'r1',
    connection_id: null,
    connection: null,
    factory_id: 'f1',
    environment: 'default',
    server: 'linear',
    tool: 'create_comment',
    agent: 'architect',
    created_by: { kind: 'user', id: 'u1', name: 'Freddy Cruz' },
    created_at: '2026-10-30T09:12:00Z',
    item_id: 'item-1',
    revoked_by: null,
    revoked_at: null,
    ...over,
  };
}

function renderCard(rules: StandingRule[]) {
  api.list.mockResolvedValue(rules);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const invalidate = vi.spyOn(client, 'invalidateQueries');
  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <McpCard
          environment="default"
          spec={readEnv(file)}
          change={() => undefined}
          readOnly={false}
          results={{ linear }}
          testing={undefined}
          testServer={() => undefined}
        />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole('button', { name: /linear/ }));
  return { invalidate };
}

beforeEach(() => {
  holds.manage = true;
  api.list.mockReset();
  api.revoke.mockReset();
  toast.success.mockClear();
  toast.error.mockClear();
});
afterEach(cleanup);

describe('the standing rules of an MCP server', () => {
  it('show only the active rules of this factory, environment and server, under their tool group', async () => {
    renderCard([
      rule({ id: 'a' }),
      rule({ id: 'b', tool: 'list_issues', agent: 'reviewer' }),
      rule({ id: 'c', factory_id: 'f2', tool: 'delete_issue' }),
      rule({ id: 'd', environment: 'deploy', tool: 'delete_issue' }),
      rule({ id: 'e', server: 'sentry', tool: 'delete_issue' }),
      rule({ id: 'f', tool: 'delete_issue', revoked_at: '2026-10-31T10:00:00Z' }),
      rule({ id: 'g', connection_id: 'c1', factory_id: null, environment: null, server: null }),
    ]);
    await screen.findAllByRole('button', { name: /^Revoke/ });
    const groupOf = (tool: string) =>
      screen.getByRole('button', { name: new RegExp(`^Revoke ${tool} `) }).closest('li[class*=border-b]');
    const change = groupOf('create_comment')?.textContent ?? '';
    expect(change).toContain('Change');
    expect(change).toContain('create_comment: Always allowed for architect, by Freddy Cruz on Oct 30');
    const read = groupOf('list_issues')?.textContent ?? '';
    expect(read).toContain('Read');
    expect(read).toContain('list_issues: Always allowed for reviewer');
    expect(screen.getAllByRole('button', { name: /^Revoke/ })).toHaveLength(2);
  });

  it('keeps a rule for a tool the server no longer lists', async () => {
    renderCard([rule({ tool: 'old_tool' })]);
    expect(await screen.findByText(/old_tool/, { selector: 'span.font-mono' })).toBeTruthy();
  });

  it('hide Revoke without connection:manage', async () => {
    holds.manage = false;
    renderCard([rule({})]);
    await screen.findByText(/Always allowed for architect/);
    expect(screen.queryByRole('button', { name: /^Revoke/ })).toBeNull();
  });

  it('revoke through the API, refresh the list, remove the row and say so', async () => {
    api.revoke.mockImplementation(async () => {
      api.list.mockResolvedValue([]);
      return rule({ revoked_at: '2026-11-03T10:00:00Z' });
    });
    const { invalidate } = renderCard([rule({})]);
    fireEvent.click(await screen.findByRole('button', { name: 'Revoke create_comment for architect' }));
    await waitFor(() => expect(api.revoke).toHaveBeenCalledWith('o1', 'r1'));
    await waitFor(() => expect(toast.success).toHaveBeenCalled());
    await waitFor(() => expect(invalidate).toHaveBeenCalledWith({ queryKey: ['org', 'o1', 'standing-rules'] }));
    await waitFor(() => expect(screen.queryByText(/Always allowed for architect/)).toBeNull());
  });

  it('read a 409 standing-rule-revoked answer in a toast and refresh the list', async () => {
    api.revoke.mockRejectedValue(
      new ApiError({
        title: 'Conflict',
        status: 409,
        code: 'standing-rule-revoked',
        detail: 'this rule was revoked already',
      }),
    );
    const { invalidate } = renderCard([rule({})]);
    fireEvent.click(await screen.findByRole('button', { name: 'Revoke create_comment for architect' }));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('this rule was revoked already'));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['org', 'o1', 'standing-rules'] });
  });

  it('show a placeholder while the list loads', () => {
    api.list.mockReturnValue(new Promise(() => undefined));
    render(
      <QueryClientProvider client={new QueryClient()}>
        <McpCard
          environment="default"
          spec={readEnv(file)}
          change={() => undefined}
          readOnly={false}
          results={{ linear }}
          testing={undefined}
          testServer={() => undefined}
        />
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByRole('button', { name: /linear/ }));
    expect(document.querySelector('[aria-busy="true"]')).not.toBeNull();
  });
});

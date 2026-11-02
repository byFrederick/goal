// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { toast } from 'sonner';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { connectionKindsQuery, connectionQuery } from '../../api/queries';
import type { Connection, ConnectionTestResult, OrgSummary } from '../../api/types';
import { connectionFixture, kindFixture, renderConnectionsRoute } from '../../test/connections-route';
import { ConnectionPage } from './connection-page';

// The top bar is the app's shell: here the page's actions render in a region named Top bar, where the page puts them.
vi.mock('../../app/top-bar', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../app/top-bar')>()),
  TopBarSlot: ({ actions }: { actions?: React.ReactNode }) => <section aria-label="Top bar">{actions}</section>,
}));

// The connection and its kind are seeded; the test request is held until a test releases it.
const api = vi.hoisted(() => ({ release: undefined as ((result: ConnectionTestResult) => void) | undefined }));

vi.mock(import('../../api/connections'), async (original) => ({
  ...(await original()),
  // The test's answer refreshes the connection lists, which the page waits on: they answer from the fixture.
  listConnections: vi.fn(async () => []),
  getConnection: vi.fn(async () => connectionFixture({ name: 'datadog-prod', kind: 'datadog', last_test: passed() })),
  testConnection: vi.fn(
    () =>
      new Promise<ConnectionTestResult>((resolve) => {
        api.release = resolve;
      }),
  ),
}));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  api.release = undefined;
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: 'owner' };
const manage = ['connection:manage', 'connection:read'];
const kind = kindFixture({
  name: 'datadog',
  title: 'Datadog',
  testable: true,
  fields: [{ name: 'api_key', title: 'API key', required: true, secret: true }],
});

function renderConnection(connection: Connection, search?: Record<string, string>) {
  return renderConnectionsRoute({
    page: 'list',
    org,
    can: manage,
    connection: connection.name,
    search,
    seed: [
      [connectionQuery(org.id, connection.name).queryKey, connection],
      [connectionKindsQuery(org.id).queryKey, [kind]],
    ],
    components: { list: () => null, add: () => null, connection: ConnectionPage },
  });
}

const passed = (): ConnectionTestResult => ({ ok: true, status: 200, at: new Date().toISOString() });

describe('the connection page', () => {
  it('puts Test in the top bar, once, before any test ran', async () => {
    renderConnection(connectionFixture({ name: 'datadog-prod', kind: 'datadog' }));

    const bar = await screen.findByRole('region', { name: 'Top bar' });
    expect(within(bar).getByRole('button', { name: 'Test' })).toBeTruthy();
    expect(screen.getAllByRole('button', { name: 'Test' })).toHaveLength(1);
  });

  it('leaves the top bar empty once a test result holds Test again', async () => {
    renderConnection(connectionFixture({ name: 'datadog-prod', kind: 'datadog', last_test: passed() }));

    const bar = await screen.findByRole('region', { name: 'Top bar' });
    expect(within(bar).queryByRole('button')).toBeNull();
  });

  it('offers Test again inside the last test’s result, and shows Testing… while it runs', async () => {
    const { testConnection } = await import('../../api/connections');
    renderConnection(connectionFixture({ name: 'datadog-prod', kind: 'datadog', last_test: passed() }));

    fireEvent.click(await screen.findByRole('button', { name: 'Test again' }));

    await waitFor(() => expect(testConnection).toHaveBeenCalledWith('org-1', 'datadog-prod'));
    const pending = await screen.findByRole('button', { name: 'Testing…' });
    expect(pending.hasAttribute('disabled')).toBe(true);

    api.release?.(passed());
    expect(await screen.findByRole('button', { name: 'Test again' })).toBeTruthy();
    expect(toast.success).not.toHaveBeenCalledWith(expect.stringContaining('is created'), expect.anything());
  });

  it('says a new connection is created in a toast, not a banner in the page', async () => {
    renderConnection(connectionFixture({ name: 'datadog-prod', kind: 'datadog' }), { created: 'true' });

    await waitFor(() =>
      expect(toast.success).toHaveBeenCalledWith(
        'datadog-prod is created',
        expect.objectContaining({ description: 'Test it to check what you entered works.' }),
      ),
    );
    expect(screen.queryByRole('status')).toBeNull();
  });
});

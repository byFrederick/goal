// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { useMemo, useState } from 'react';
import { toast } from 'sonner';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { connectionKindsQuery, connectionQuery } from '../../api/queries';
import type { Connection, ConnectionTestResult, OrgSummary } from '../../api/types';
import { TopBarSetContext, type TopBarState } from '../../app/top-bar';
import { connectionFixture, kindFixture, renderConnectionsRoute } from '../../test/connections-route';
import { ConnectionPage } from './connection-page';

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

/** The page under a top bar that shows what the page sets, as the app's shell does. */
function PageInShell() {
  const [bar, setBar] = useState<TopBarState | null>(null);
  // As in the shell, the page is not re-rendered by the bar it sets.
  const page = useMemo(() => <ConnectionPage />, []);
  return (
    <TopBarSetContext.Provider value={setBar}>
      <div data-testid="bar">{bar?.actions}</div>
      {page}
    </TopBarSetContext.Provider>
  );
}

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
    components: { list: () => null, add: () => null, connection: PageInShell },
  });
}

const passed = (): ConnectionTestResult => ({ ok: true, status: 200, at: new Date().toISOString() });

describe('the connection page', () => {
  it('sets one Test button in the top bar, shows Testing… while it runs, and a check when it passes', async () => {
    const { testConnection } = await import('../../api/connections');
    renderConnection(connectionFixture({ name: 'datadog-prod', kind: 'datadog', last_test: passed() }));

    const bar = await screen.findByTestId('bar');
    fireEvent.click(within(bar).getByRole('button', { name: 'Test' }));

    await waitFor(() => expect(testConnection).toHaveBeenCalledWith('org-1', 'datadog-prod'));
    const pending = await screen.findByRole('button', { name: 'Testing…' });
    expect(pending.hasAttribute('disabled')).toBe(true);

    api.release?.(passed());
    const done = await screen.findByRole('button', { name: 'Test' });
    expect(done.querySelector('svg.text-ok')).not.toBeNull();
    expect(screen.queryByRole('button', { name: 'Test again' })).toBeNull();
    expect(toast.success).not.toHaveBeenCalledWith(expect.stringContaining('is created'), expect.anything());
  });

  it('offers Test before any result too, and keeps the result text in the page', async () => {
    renderConnection(connectionFixture({ name: 'datadog-prod', kind: 'datadog' }));

    expect(within(await screen.findByTestId('bar')).getByRole('button', { name: 'Test' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Test again' })).toBeNull();
  });

  it('shows a 20 px title, no body breadcrumb, one column of fields and 14 px section headings', async () => {
    renderConnection(connectionFixture({ name: 'datadog-prod', kind: 'datadog', last_test: passed() }));

    const h1 = await screen.findByRole('heading', { level: 1 });
    expect(h1.className).toContain('text-page-title');
    expect(screen.queryByRole('navigation', { name: 'Breadcrumb' })).toBeNull();
    for (const name of ['Fields', 'Used by', 'What a run gets']) {
      const h2 = screen.getByRole('heading', { level: 2, name });
      expect(h2.className).toContain('text-heading');
      expect(h2.className).not.toContain('text-lg');
    }
    expect(document.querySelector('[class*="grid-cols-2"]')).toBeNull();
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

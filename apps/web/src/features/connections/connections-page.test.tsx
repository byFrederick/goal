// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { connectionKindsQuery } from '../../api/queries';
import type { Connection, ConnectionKind, OrgSummary } from '../../api/types';
import { connectionFixture, kindFixture, renderConnectionsRoute } from '../../test/connections-route';
import { ConnectionsPage } from './connections-page';

// The list reads its connections and kinds from the API; a test sets what they hold, or makes them
// hang or fail for the loading and error states.
const api = vi.hoisted(() => ({
  connections: [] as Connection[],
  hold: false,
  fail: false,
}));

vi.mock(import('../../api/connections'), async (original) => ({
  ...(await original()),
  listConnections: vi.fn(async () => {
    if (api.hold) return new Promise<never>(() => {});
    if (api.fail) throw new Error('The server is busy');
    return api.connections;
  }),
  listConnectionKinds: vi.fn(async () => kinds),
}));

afterEach(() => {
  cleanup();
  api.connections = [];
  api.hold = false;
  api.fail = false;
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: 'owner' };
const manage = ['connection:manage', 'connection:read'];

const kinds: ConnectionKind[] = [
  kindFixture({
    name: 'github',
    title: 'GitHub',
    code_host: 'github',
    modes: ['env', 'git'],
    fields: [{ name: 'token', title: 'Token', secret: true, required: true }],
  }),
  kindFixture({ name: 'anthropic', title: 'Anthropic API key', model: true }),
  kindFixture({ name: 'datadog', title: 'Datadog' }),
  kindFixture({ name: 'mcp-server', title: 'MCP server', modes: ['mcp'], testable: false }),
];

const connections: Connection[] = [
  connectionFixture({ name: 'github', kind: 'github', status: 'ok', description: 'acme-bot' }),
  connectionFixture({ name: 'claude', kind: 'anthropic', status: 'ok' }),
  connectionFixture({ name: 'datadog', kind: 'datadog', status: 'ok' }),
  connectionFixture({ name: 'pagerduty', kind: 'datadog', status: 'failing' }),
  connectionFixture({ name: 'context7', kind: 'mcp-server', status: 'untested', waiting_approvals: 2 }),
];

/** Renders the list at /o/acme/connections with the connections the API answers. */
function renderList({ can = manage, search }: { can?: string[]; search?: Record<string, string> } = {}) {
  api.connections = connections;
  return renderConnectionsRoute({
    page: 'list',
    org,
    can,
    search,
    withTopBar: true,
    components: { list: ConnectionsPage, add: () => null, connection: () => null },
    seed: [[connectionKindsQuery(org.id).queryKey, kinds]],
  });
}

/** The section under a group heading, found by its name. */
function group(name: string): HTMLElement {
  return screen.getByRole('heading', { level: 2, name }).closest('section') as HTMLElement;
}

describe('the Connections list', () => {
  it('groups the connections by kind: code hosts, apps, MCP servers and model providers', async () => {
    renderList();
    await screen.findByRole('heading', { level: 2, name: 'Code hosts' });

    const headings = screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent);
    expect(headings).toEqual(['Code hosts', 'Apps', 'MCP servers', 'Model providers']);
    // The table and the list both render; jsdom applies no container query, so each is in the DOM.
    expect(within(group('Code hosts')).getAllByRole('link', { name: 'github' })).not.toHaveLength(0);
    expect(within(group('Apps')).getAllByRole('link', { name: 'pagerduty' })).not.toHaveLength(0);
    expect(within(group('MCP servers')).getAllByRole('link', { name: 'context7' })).not.toHaveLength(0);
    expect(within(group('Model providers')).getAllByRole('link', { name: 'claude' })).not.toHaveLength(0);
  });

  it('hides a group with no connection in it', async () => {
    api.connections = connections.filter((c) => c.kind !== 'mcp-server');
    renderConnectionsRoute({
      page: 'list',
      org,
      can: manage,
      components: { list: ConnectionsPage, add: () => null, connection: () => null },
      seed: [[connectionKindsQuery(org.id).queryKey, kinds]],
    });
    await screen.findByRole('heading', { level: 2, name: 'Apps' });
    expect(screen.queryByRole('heading', { level: 2, name: 'MCP servers' })).toBeNull();
  });

  it('shows the Connected and Need attention tiles, and the writes waiting across the connections, linking to Needs you', async () => {
    renderList();
    const connected = (await screen.findAllByText('Connected')).find((e) => e.className.includes('text-secondary'));
    expect(within((connected as HTMLElement).parentElement as HTMLElement).getAllByText('3')).not.toHaveLength(0);
    expect(within(screen.getByText('Need attention').parentElement as HTMLElement).getAllByText('1')).not.toHaveLength(
      0,
    );

    const writes = screen.getByRole('link', { name: /Writes waiting for your OK/ });
    expect(within(writes).getAllByText('2')).not.toHaveLength(0);
    expect(writes.getAttribute('href')).toBe('/o/acme/needs-you');
  });

  it('calls out the failing connection above the list, with Replace secret…', async () => {
    renderList();
    const alert = await screen.findByRole('region', { name: 'pagerduty failed its test' });
    expect(within(alert).getByRole('button', { name: 'Replace secret…' })).toBeTruthy();
  });

  it('says the loading state to a screen reader while the connections load', async () => {
    api.hold = true;
    renderList();
    expect(await screen.findByText('Loading')).toBeTruthy();
    expect(screen.getByText('Loading').closest('[aria-busy="true"]')).not.toBeNull();
  });

  it('says the error and offers to try again when the connections do not load', async () => {
    api.fail = true;
    renderList();
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('Couldn’t load connections');
    expect(within(alert).getByRole('button', { name: 'Try again' })).toBeTruthy();
  });

  it('shows the empty state with the way to add a connection when there are none', async () => {
    api.connections = [];
    renderConnectionsRoute({
      page: 'list',
      org,
      can: manage,
      components: { list: ConnectionsPage, add: () => null, connection: () => null },
      seed: [[connectionKindsQuery(org.id).queryKey, kinds]],
    });
    expect(await screen.findByText('No connections yet')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Add connection' })).toBeTruthy();
  });

  it('searches from the top bar, narrows the rows and keeps the query in the URL', async () => {
    const { router } = renderList();
    const banner = await screen.findByTestId('top-bar');
    const field = within(banner).getByRole('searchbox', { name: 'Search connections' });
    expect(field.getAttribute('placeholder')).toBe('Search connections');
    expect(screen.queryByRole('combobox')).toBeNull();
    fireEvent.change(field, { target: { value: 'git' } });
    await waitFor(() => expect(router.state.location.search).toMatchObject({ q: 'git' }));
    await waitFor(() => expect(screen.queryByRole('heading', { level: 2, name: 'Model providers' })).toBeNull());
    expect(within(group('Code hosts')).getAllByRole('link', { name: 'github' })).not.toHaveLength(0);
  });

  it('opens with the query of the URL applied', async () => {
    renderList({ search: { q: 'git' } });
    await screen.findByRole('heading', { level: 2, name: 'Code hosts' });
    expect(screen.queryByRole('heading', { level: 2, name: 'Apps' })).toBeNull();
    expect(screen.queryByRole('heading', { level: 2, name: 'Model providers' })).toBeNull();
  });

  it('shows a zero in ink and a figure above zero in its warning colour', async () => {
    api.connections = connections.filter((c) => c.status === 'ok');
    renderConnectionsRoute({
      page: 'list',
      org,
      can: manage,
      components: { list: ConnectionsPage, add: () => null, connection: () => null },
      seed: [[connectionKindsQuery(org.id).queryKey, kinds]],
    });
    const zero = (await screen.findByText('Need attention')).parentElement?.querySelector('.text-kpi');
    expect(zero?.className).toContain('text-ink');
    expect(zero?.className).not.toContain('text-rework-ink');
    const writes = screen.getByRole('link', { name: /Writes waiting for your OK/ }).querySelector('.text-kpi');
    expect(writes?.className).not.toContain('text-you-ink');
  });

  it('says Connected beside a dot for a working connection, and Test failed: for a failing one', async () => {
    renderList();
    const row = (await screen.findAllByRole('row')).find((r) => within(r).queryByRole('link', { name: 'github' }));
    expect(within(row as HTMLElement).getByText('Connected')).toBeTruthy();
    expect(row?.textContent).not.toContain('OK');
  });

  it('names the factories that use a connection with letter badges, and the row action by connection', async () => {
    api.connections = [
      connectionFixture({
        name: 'github',
        kind: 'github',
        status: 'ok',
        used_by: { factories: [{ id: 'f-1', name: 'sre' }], knowledge_sources: [] },
      }),
    ];
    renderConnectionsRoute({
      page: 'list',
      org,
      can: manage,
      components: { list: ConnectionsPage, add: () => null, connection: () => null },
      seed: [[connectionKindsQuery(org.id).queryKey, kinds]],
    });
    const row = (await screen.findAllByRole('row')).find((r) => within(r).queryByRole('link', { name: 'github' }));
    expect(within(row as HTMLElement).getByText('sre', { selector: '.sr-only' })).toBeTruthy();
    expect(within(row as HTMLElement).getByText('S')).toBeTruthy();
    expect(within(row as HTMLElement).getByRole('button', { name: 'Actions for github' })).toBeTruthy();
  });

  it('hides Add connection from a member who cannot manage connections', async () => {
    renderList({ can: ['connection:read'] });
    await screen.findByRole('heading', { level: 2, name: 'Code hosts' });
    expect(screen.queryByRole('link', { name: 'Add connection' })).toBeNull();
  });
});

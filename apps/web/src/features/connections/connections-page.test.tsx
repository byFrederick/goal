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
  connectionFixture({ name: 'context7', kind: 'mcp-server', status: 'untested' }),
];

/** Renders the list at /o/acme/connections with the connections the API answers. */
function renderList({ can = manage, search }: { can?: string[]; search?: Record<string, string> } = {}) {
  api.connections = connections;
  return renderConnectionsRoute({
    page: 'list',
    org,
    can,
    search,
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
    await screen.findByRole('heading', { level: 2, name: '5 connections' });

    const headings = screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent);
    expect(headings).toEqual([
      '5 connections',
      'Code hosts',
      'Apps',
      'MCP servers',
      'Model providers',
      'How runs get connections',
    ]);
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

  it('shows the Connected and Need attention tiles, and the writes tile disabled as not available yet', async () => {
    renderList();
    const connected = await screen.findByText('Connected');
    expect(within(connected.parentElement as HTMLElement).getByText('3')).toBeTruthy();
    expect(within(screen.getByText('Need attention').parentElement as HTMLElement).getByText('1')).toBeTruthy();

    const writes = screen.getByRole('button', { name: /Writes waiting for your OK/ });
    expect(writes.getAttribute('aria-disabled')).toBe('true');
    writes.focus();
    expect((await screen.findAllByText('Not available yet')).length).toBeGreaterThan(0);
  });

  it('calls out the failing connection above the list, with Replace secret…', async () => {
    renderList();
    const alert = await screen.findByRole('region', { name: 'pagerduty failed its test' });
    expect(within(alert).getByRole('button', { name: 'Replace secret…' })).toBeTruthy();
  });

  it('says the loading state to a screen reader while the connections load', async () => {
    api.hold = true;
    renderList();
    expect((await screen.findByRole('status')).textContent).toBe('Loading connections');
    expect(screen.getByRole('status').closest('[aria-busy="true"]')).toBeTruthy();
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

  it('filters by the kind chosen in the URL and says how many match', async () => {
    const { router } = renderList();
    await screen.findByRole('heading', { level: 2, name: '5 connections' });
    fireEvent.change(screen.getByRole('combobox', { name: 'Kind' }), { target: { value: 'datadog' } });
    await waitFor(() => expect(router.state.location.search).toMatchObject({ kind: 'datadog' }));
    expect(await screen.findByRole('heading', { level: 2, name: '2 of 5 connections' })).toBeTruthy();
  });

  it('hides Add connection from a member who cannot manage connections', async () => {
    renderList({ can: ['connection:read'] });
    await screen.findByRole('heading', { level: 2, name: '5 connections' });
    expect(screen.queryByRole('link', { name: 'Add connection' })).toBeNull();
  });
});

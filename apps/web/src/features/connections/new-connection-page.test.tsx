// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createConnection } from '../../api/connections';
import { connectionKindsQuery } from '../../api/queries';
import type { ConnectionKind, OrgSummary } from '../../api/types';
import { reasons } from '../../shared/permissions';
import { connectionFixture, kindFixture, renderConnectionsRoute } from '../../test/connections-route';
import { NewConnectionPage } from './new-connection-page';

// The kinds load from the API; a test makes them hang or fail for the loading and error states.
// The create request is mocked: it answers the connection it was asked for.
const api = vi.hoisted(() => ({ hold: false, fail: false }));

vi.mock(import('../../api/connections'), async (original) => ({
  ...(await original()),
  listConnectionKinds: vi.fn(async () => {
    if (api.hold) return new Promise<never>(() => {});
    if (api.fail) throw new Error('The server is busy');
    return kinds;
  }),
  listConnections: vi.fn(async () => []),
  createConnection: vi.fn(async (_org: string, body: { name: string; kind: string }) =>
    connectionFixture({ name: body.name, kind: body.kind }),
  ),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  api.hold = false;
  api.fail = false;
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: 'owner' };
const manage = ['connection:manage', 'connection:read'];

const kinds: ConnectionKind[] = [
  kindFixture({
    name: 'datadog',
    title: 'Datadog',
    description: 'Metrics, logs and monitors',
    fields: [
      { name: 'site', title: 'Site', required: false, secret: false, default: 'datadoghq.com' },
      { name: 'api_key', title: 'API key', required: true, secret: true },
    ],
  }),
  kindFixture({
    name: 'mcp-server',
    title: 'MCP server',
    description: 'An MCP server without credentials',
    modes: ['mcp'],
    testable: false,
    fields: [{ name: 'url', title: 'URL', required: true, secret: false }],
  }),
  kindFixture({ name: 'slack', title: 'Slack', platform: false, description: 'Our Slack bot' }),
];

/** Renders the add page at /o/acme/connections/new, with `kind` in the URL when given. */
function renderAdd({ kind, can = manage }: { kind?: string; can?: string[] } = {}) {
  return renderConnectionsRoute({
    page: 'new',
    org,
    can,
    search: kind ? { kind } : undefined,
    components: { list: () => null, add: NewConnectionPage, connection: () => <p>Connection page</p> },
    seed: [[connectionKindsQuery(org.id).queryKey, kinds]],
  });
}

describe('the Add a connection page', () => {
  it('lists the catalog and shows the chosen kind’s title, description and fields', async () => {
    const { router } = renderAdd();
    const catalog = await screen.findByRole('navigation', { name: 'Catalog' });
    expect(within(catalog).getByText('Catalog')).toBeTruthy();
    expect(within(catalog).getByText('Your own')).toBeTruthy();

    fireEvent.click(within(catalog).getByRole('button', { name: /^Datadog/ }));
    await waitFor(() => expect(router.state.location.search).toMatchObject({ kind: 'datadog' }));

    expect(await screen.findByRole('heading', { level: 2, name: 'Datadog' })).toBeTruthy();
    expect(screen.getByText('Metrics, logs and monitors')).toBeTruthy();
    const form = screen.getByRole('form', { name: 'Datadog details' });
    expect(within(form).getByLabelText('API key').getAttribute('type')).toBe('password');
    expect(
      within(catalog)
        .getByRole('button', { name: /^Datadog/ })
        .getAttribute('aria-current'),
    ).toBe('true');
  });

  it('says the required field is missing and keeps the connection from being added', async () => {
    renderAdd({ kind: 'datadog' });
    const form = await screen.findByRole('form', { name: 'Datadog details' });
    fireEvent.click(within(form).getByRole('button', { name: 'Add Datadog' }));
    expect(await within(form).findByText('API key is required.')).toBeTruthy();
    expect(createConnection).not.toHaveBeenCalled();
  });

  it('adds the connection with its name and fields, then opens it', async () => {
    const { router } = renderAdd({ kind: 'datadog' });
    const form = await screen.findByRole('form', { name: 'Datadog details' });
    fireEvent.change(within(form).getByLabelText('API key'), { target: { value: 'test-api-key' } });
    fireEvent.click(within(form).getByRole('button', { name: 'Add Datadog' }));

    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/connections/datadog'));
    expect(createConnection).toHaveBeenCalledWith(
      'org-1',
      expect.objectContaining({ name: 'datadog', kind: 'datadog', secrets: { api_key: 'test-api-key' } }),
    );
    expect(await screen.findByText('Connection page')).toBeTruthy();
  });

  it('shows the tool setting for an MCP server disabled, saying it is not available yet', async () => {
    renderAdd({ kind: 'mcp-server' });
    const askFirst = await screen.findByRole('button', { name: 'Ask first' });
    expect(askFirst.getAttribute('aria-disabled')).toBe('true');
    askFirst.focus();
    expect((await screen.findAllByText('Not available yet')).length).toBeGreaterThan(0);
    fireEvent.click(askFirst);
    expect(askFirst.getAttribute('aria-pressed')).toBe('false');
  });

  it('marks a kind the organization defined as its own', async () => {
    renderAdd();
    const catalog = await screen.findByRole('navigation', { name: 'Catalog' });
    expect(within(catalog).getByRole('button', { name: /^Slack/ })).toBeTruthy();
    expect(within(catalog).getByRole('button', { name: 'Define a kind…' })).toBeTruthy();
  });

  it('says the loading state to a screen reader while the kinds load', async () => {
    api.hold = true;
    renderConnectionsRoute({
      page: 'new',
      org,
      can: manage,
      components: { list: () => null, add: NewConnectionPage, connection: () => null },
    });
    expect((await screen.findByRole('status')).textContent).toBe('Loading connection kinds');
  });

  it('says the error and offers to try again when the kinds do not load', async () => {
    api.fail = true;
    renderConnectionsRoute({
      page: 'new',
      org,
      can: manage,
      components: { list: () => null, add: NewConnectionPage, connection: () => null },
    });
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('Couldn’t load the connection kinds');
    expect(within(alert).getByRole('button', { name: 'Try again' })).toBeTruthy();
  });

  it('shows the kinds and the choice of one, with no form for a member who cannot add', async () => {
    renderAdd({ kind: 'datadog', can: ['connection:read'] });
    expect(await screen.findByRole('heading', { level: 2, name: 'Datadog' })).toBeTruthy();
    expect(screen.queryByRole('form', { name: 'Datadog details' })).toBeNull();
    expect(screen.getByText(reasons.connections(org.name))).toBeTruthy();
  });
});

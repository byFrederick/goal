// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { toast } from 'sonner';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../api/client';
import { deleteCredential, updateCredential } from '../../api/credentials';
import { factoriesQuery, orgQuery } from '../../api/queries';
import type { Credential, Factory, Org, OrgSummary } from '../../api/types';
import { renderOrgPage } from '../../test/route';
import { TooltipProvider } from '../../ui/tooltip';
import { CredentialsPage } from './credentials-page';

// The list the page reads, and which requests fail. The clock is fixed so the expiry days and the
// "expires this week" count do not depend on the day the test runs.
const state = vi.hoisted(() => ({
  rows: [] as unknown[],
  fail: false,
  hold: false,
}));

vi.mock('../../api/credentials', () => ({
  listCredentials: vi.fn(async () => {
    if (state.hold) return new Promise<never>(() => {});
    if (state.fail) throw new Error('The server is busy');
    return state.rows;
  }),
  updateCredential: vi.fn(async (_org: string, name: string) => ({ name })),
  deleteCredential: vi.fn(async () => undefined),
}));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const NOW = Date.parse('2026-10-27T12:00:00Z');
const DAY = 86_400_000;
const iso = (ms: number) => new Date(ms).toISOString();

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.useRealTimers();
  state.rows = [];
  state.fail = false;
  state.hold = false;
});

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(NOW);
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 3, role: null };
const factories = [
  { id: 'f-software', name: 'software' },
  { id: 'f-sre', name: 'sre' },
  { id: 'f-support', name: 'support' },
] as unknown as Factory[];

const by = { kind: 'user' as const, id: 'u', name: 'U' };
const row = (over: Partial<Credential>): Credential =>
  ({
    id: `id-${over.name}`,
    description: '',
    scope: { all_factories: true, factories: [] },
    agents: {},
    expires_at: null,
    remind_days: 0,
    last_used_at: null,
    used_by: { connections: [], agents: [] },
    created_by: by,
    created_at: iso(NOW - 90 * DAY),
    updated_by: by,
    updated_at: iso(NOW - 90 * DAY),
    ...over,
  }) as Credential;

/** The picture's seven rows, with the expiry the API would carry for each. */
const picture = (): Credential[] => [
  row({
    name: 'github-app',
    type: 'app_key',
    expires_at: iso(NOW + 46 * DAY),
    remind_days: 14,
    last_used_at: iso(NOW - 2 * 60_000),
    used_by: { connections: ['github'], agents: [] },
  }),
  row({
    name: 'cloudflare-token',
    type: 'api_key',
    scope: { all_factories: false, factories: ['f-software', 'f-sre'] },
    expires_at: iso(NOW + 2 * DAY),
    remind_days: 7,
    last_used_at: iso(NOW - 3_600_000),
    used_by: { connections: ['cloudflare'], agents: [] },
  }),
  row({
    name: 'claude-subscription',
    type: 'oauth_token',
    last_used_at: iso(NOW),
    used_by: { connections: ['claude'], agents: [] },
  }),
  row({
    name: 'zendesk-api',
    type: 'api_key',
    scope: { all_factories: false, factories: ['f-support'] },
    expires_at: iso(NOW + 69 * DAY),
    remind_days: 14,
    last_used_at: iso(NOW - 2 * 3_600_000),
    used_by: { connections: ['zendesk'], agents: [] },
  }),
  row({
    name: 'runbooks-token',
    type: 'api_key',
    scope: { all_factories: false, factories: ['f-sre'] },
    last_used_at: iso(NOW - DAY),
    used_by: { connections: ['runbooks'], agents: [] },
  }),
  row({
    name: 'deploy-preview-ssh',
    type: 'ssh_key',
    scope: { all_factories: false, factories: ['f-software'] },
    last_used_at: iso(NOW - 7 * DAY),
    used_by: { connections: [], agents: [{ factory_id: 'f-software', agent: 'infra-engineer' }] },
  }),
  row({
    name: 'gcp-readonly',
    type: 'cloud_role',
    scope: { all_factories: false, factories: ['f-sre'] },
    last_used_at: iso(NOW - 3 * 3_600_000),
    used_by: { connections: [], agents: [{ factory_id: 'f-sre', agent: 'investigator' }] },
  }),
];

function renderPage({
  can = ['credential:read', 'credential:manage'],
  search,
}: {
  can?: string[];
  search?: Record<string, string>;
} = {}) {
  const grants = { ...org, created_at: '2026-09-01T00:00:00Z', can, role: 'owner' } as unknown as Org;
  return renderOrgPage({
    page: () => (
      <TooltipProvider>
        <CredentialsPage />
      </TooltipProvider>
    ),
    org,
    seed: [
      [orgQuery(org.id).queryKey, grants],
      [factoriesQuery(org.id).queryKey, factories],
    ],
    search,
    path: '/credentials',
  });
}

/** The row of one credential, by its name in the first cell. */
const rowOf = (name: string) => screen.getByText(name).closest('tr') as HTMLElement;

/** Opens a menu by its trigger, the way a pointer does (Radix opens on pointer down). */
const openMenu = (name: RegExp | string) =>
  fireEvent.pointerDown(screen.getByRole('button', { name }), { button: 0, ctrlKey: false, pointerType: 'mouse' });

const isChecked = (name: RegExp) => (screen.getByRole('radio', { name }) as HTMLInputElement).checked;

describe('the Credentials page', () => {
  it('says it is loading while the list is out', async () => {
    state.hold = true;
    renderPage();

    const status = await screen.findByRole('status');
    expect(status.textContent).toBe('Loading credentials');
    expect(status.closest('[aria-busy="true"]')).not.toBeNull();
  });

  it('shows the error with a way to try again', async () => {
    state.rows = picture();
    state.fail = true;
    renderPage();

    expect(await screen.findByRole('alert')).toBeTruthy();
    state.fail = false;
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));

    expect(await screen.findByRole('table', { name: /Credentials of Acme/ })).toBeTruthy();
  });

  it('says there are no credentials yet and offers New credential to a manager', async () => {
    renderPage();

    expect(await screen.findByText('No credentials yet')).toBeTruthy();
    expect(screen.getAllByRole('button', { name: /New credential/ }).length).toBeGreaterThan(0);
  });

  it('shows the picture’s seven rows with their type, factories, users, last use and expiry', async () => {
    state.rows = picture();
    renderPage();

    const table = await screen.findByRole('table', { name: /Credentials of Acme/ });
    expect(within(table).getAllByRole('row')).toHaveLength(8);
    expect(within(rowOf('github-app')).getByText('App key')).toBeTruthy();
    expect(within(rowOf('github-app')).getByText('All factories')).toBeTruthy();
    expect(within(rowOf('github-app')).getByText('Connection github')).toBeTruthy();
    expect(within(rowOf('github-app')).getByText('Expires Dec 12')).toBeTruthy();
    expect(within(rowOf('cloudflare-token')).getByText('software')).toBeTruthy();
    expect(within(rowOf('deploy-preview-ssh')).getByText('infra-engineer')).toBeTruthy();
    expect(within(rowOf('runbooks-token')).getByText('Never expires')).toBeTruthy();
    expect(within(rowOf('gcp-readonly')).getByText('investigator')).toBeTruthy();
  });

  it('keeps the type tab in the URL and filters the rows by it', async () => {
    state.rows = picture();
    const { router } = renderPage();

    fireEvent.click(await screen.findByRole('radio', { name: /OAuth tokens/ }));
    await waitFor(() => expect(router.state.location.search).toEqual({ type: 'oauth_token' }));

    expect(router.state.location.search).toEqual({ type: 'oauth_token' });
    await waitFor(() => expect(isChecked(/OAuth tokens/)).toBe(true));
    expect(within(screen.getByRole('table')).getAllByRole('row')).toHaveLength(2);
  });

  it('opens the URL with a type selected and its rows only', async () => {
    state.rows = picture();
    renderPage({ search: { type: 'api_key' } });

    await screen.findByRole('table');
    expect(isChecked(/API keys/)).toBe(true);
    expect(within(screen.getByRole('table')).queryByText('gcp-readonly')).toBeNull();
    expect(within(screen.getByRole('table')).getByText('zendesk-api')).toBeTruthy();
  });

  it('says so when no credential matches the filters, and clears them', async () => {
    state.rows = [row({ name: 'github-app', type: 'app_key' })];
    const { router } = renderPage({ search: { type: 'ssh_key' } });

    expect(await screen.findByText('No credential matches these filters.')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Show every credential' }));

    await waitFor(() => expect(router.state.location.search).toEqual({}));
    expect(await screen.findByText('github-app')).toBeTruthy();
  });

  it('counts the credentials that expire this week in a chip', async () => {
    state.rows = picture();
    renderPage();

    expect(await screen.findByText('1 expires this week')).toBeTruthy();
  });

  it('says the expiry in the danger colour with Replace inside the reminder window only', async () => {
    state.rows = picture();
    renderPage();

    await screen.findByRole('table');
    const expiry = within(rowOf('cloudflare-token')).getByText('Expires Oct 29, in 2 days');
    expect(expiry.className).toContain('text-rework-ink');
    expect(within(rowOf('cloudflare-token')).getByRole('button', { name: 'Replace' })).toBeTruthy();
    expect(within(rowOf('zendesk-api')).queryByRole('button', { name: 'Replace' })).toBeNull();
    expect(within(rowOf('zendesk-api')).getByText('Expires Jan 4')).toBeTruthy();
  });

  it('hides Replace and the row menu from a member who only reads', async () => {
    state.rows = picture();
    renderPage({ can: ['credential:read'] });

    await screen.findByRole('table');
    expect(screen.queryByRole('button', { name: 'Replace' })).toBeNull();
    expect(screen.queryByRole('button', { name: /Actions for/ })).toBeNull();
  });

  it('replaces a value through the dialog and sends the new value once', async () => {
    state.rows = picture();
    renderPage();

    fireEvent.click(await screen.findByRole('button', { name: 'Replace' }));
    const dialog = await screen.findByRole('dialog', { name: /Replace the value of cloudflare-token/ });
    fireEvent.change(within(dialog).getByLabelText('Value'), { target: { value: 'test-token' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Replace value' }));

    await waitFor(() =>
      expect(updateCredential).toHaveBeenCalledWith('org-1', 'cloudflare-token', { value: 'test-token' }),
    );
    await waitFor(() => expect(toast.success).toHaveBeenCalled());
  });

  it('deletes a credential, and shows the connections that block it', async () => {
    state.rows = picture();
    vi.mocked(deleteCredential).mockRejectedValueOnce(
      new ApiError({
        type: 'credential-in-use',
        title: 'Conflict',
        status: 409,
        detail: 'credential cloudflare-token is in use by connections cloudflare',
      } as never),
    );
    renderPage();

    await screen.findByRole('table');
    openMenu('Actions for cloudflare-token');
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete…' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Delete credential' }));

    expect(await screen.findByText(/in use by connections cloudflare/)).toBeTruthy();
    expect(deleteCredential).toHaveBeenCalledWith('org-1', 'cloudflare-token');
  });

  it('links New credential to its two entries, the import one at its panel', async () => {
    state.rows = picture();
    renderPage();

    await screen.findByRole('table');
    openMenu(/New credential/);
    expect((await screen.findByRole('menuitem', { name: /Create a credential/ })).getAttribute('href')).toBe(
      '/o/acme/credentials/new',
    );
    expect(screen.getByRole('menuitem', { name: /Import from .env or YAML/ }).getAttribute('href')).toBe(
      '/o/acme/credentials/new?panel=import',
    );
  });

  it('shows encryption in the browser as not available yet', async () => {
    state.rows = picture();
    renderPage();

    const control = await screen.findByRole('button', { name: 'Encrypt in your browser instead' });
    expect(control.getAttribute('aria-disabled')).toBe('true');
  });
});

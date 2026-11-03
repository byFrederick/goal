// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { DAY, iso, NOW, renderSecrets, secret } from './test-harness';

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock('../../api/secrets', () => ({
  listSecrets: vi.fn(async () => []),
  getSecret: vi.fn(),
  listSecretActivity: vi.fn(async () => ({ entries: [], next_before: null })),
  createSecret: vi.fn(),
  importSecrets: vi.fn(),
  patchSecret: vi.fn(),
  replaceSecretValue: vi.fn(),
  deleteSecret: vi.fn(),
}));

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(NOW);
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.clearAllMocks();
});

const picture = () => [
  secret({
    name: 'github-token',
    description: 'Fine-grained GitHub token',
    last_used_at: iso(NOW - 120_000),
    expires_at: iso(NOW + 300 * DAY),
    remind_days: 14,
    uses: [
      { kind: 'definition', name: 'software' },
      { kind: 'connection', name: 'github' },
    ],
  }),
  secret({
    name: 'cloudflare-deploy',
    description: 'Workers and R2, edit',
    scope: { all_factories: false, factories: ['f-software'] },
    expires_at: iso(NOW + 3 * DAY),
    remind_days: 7,
    last_used_at: iso(NOW - 3_600_000),
    uses: [{ kind: 'definition', name: 'software' }],
  }),
  secret({ name: 'old-npm-token', description: 'npm publish token' }),
];

/** The rows of the desktop table, by the secret's name. */
const tableNow = () => screen.getByRole('table', { name: /Secrets of Acme/ });
const table = () => screen.findByRole('table', { name: /Secrets of Acme/ });

describe('the Secrets list', () => {
  it('shows each secret with its description, first use, scope, expiry and last use', async () => {
    renderSecrets({ list: picture() });

    const row = (await within(await table()).findByText('cloudflare-deploy')).closest('tr') as HTMLElement;
    expect(within(row).getByText('Workers and R2, edit')).toBeTruthy();
    expect(within(row).getByText('software · factory definition')).toBeTruthy();
    expect(within(row).getByText('software')).toBeTruthy();
    expect(within(row).getByText('In 3 days')).toBeTruthy();
    expect(within(row).getByText('1 h ago')).toBeTruthy();
    const github = within(await table())
      .getByText('github-token')
      .closest('tr') as HTMLElement;
    expect(within(github).getByText('and 1 more')).toBeTruthy();
    expect(within(github).getByText('All')).toBeTruthy();
    expect(within(await table()).getByText('Not used')).toBeTruthy();
  });

  it('warns about the secret that expires soonest, with Replace value for those who manage', async () => {
    renderSecrets({ list: picture() });

    const banner = await screen.findByRole('region', { name: 'Expiring' });
    expect(within(banner).getByText('cloudflare-deploy expires in 3 days')).toBeTruthy();
    expect(within(banner).getByRole('button', { name: 'Replace value' })).toBeTruthy();
  });

  it('counts the filters and narrows the list to Expiring and to Not used, in the URL', async () => {
    const { router } = renderSecrets({ list: picture() });

    await screen.findByRole('radio', { name: /Expiring/ });
    expect(screen.getByRole('radio', { name: /Expiring/ }).closest('label')?.textContent).toBe('Expiring 1');
    expect(screen.getByRole('radio', { name: /Not used/ }).closest('label')?.textContent).toBe('Not used 1');

    fireEvent.click(screen.getByRole('radio', { name: /Not used/ }));
    await waitFor(() => expect(router.state.location.search).toMatchObject({ filter: 'unused' }));
    expect(within(tableNow()).queryByText('github-token')).toBeNull();
    expect(within(tableNow()).getByText('old-npm-token')).toBeTruthy();
  });

  it('filters by the search text', async () => {
    renderSecrets({ list: picture() });

    fireEvent.change(await screen.findByLabelText('Search secrets'), { target: { value: 'npm' } });
    await waitFor(() => expect(within(tableNow()).queryByText('github-token')).toBeNull());
    expect(within(await table()).getByText('old-npm-token')).toBeTruthy();
  });

  it('has New secret and Import for those who manage, and neither for members', async () => {
    renderSecrets({ list: picture(), can: ['secret:read'] });

    await within(await table()).findByText('github-token');
    expect(screen.queryByRole('button', { name: /New secret/ })).toBeNull();
    expect(screen.queryByRole('link', { name: 'Import' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Replace value' })).toBeNull();
  });

  it('says there are no secrets yet', async () => {
    renderSecrets({ list: [] });

    expect(await screen.findByText('No secrets yet')).toBeTruthy();
    expect(screen.getAllByRole('button', { name: /New secret/ })).toHaveLength(1);
  });

  it('links each name to its page', async () => {
    renderSecrets({ list: picture() });

    const link = (await within(await table()).findByText('github-token')).closest('a');
    expect(link?.getAttribute('href')).toBe('/o/acme/secrets/github-token');
  });
});

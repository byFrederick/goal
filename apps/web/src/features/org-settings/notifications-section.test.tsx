// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../api/client';
import type { OrgSummary, Secret } from '../../api/types';
import { NotificationsSection } from './notifications-section';

const api = vi.hoisted(() => ({
  getOrgNotificationSettings: vi.fn(),
  setOrgNotificationSettings: vi.fn(),
  testOrgNotifications: vi.fn(),
  listSecrets: vi.fn(),
}));
vi.mock('../../api/org-notifications', () => api);
vi.mock('../../api/secrets', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/secrets')>()),
  listSecrets: api.listSecrets,
}));

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock('sonner', () => ({ toast }));

const perms = vi.hoisted(() => ({ held: new Set<string>() }));
const org = { id: 'org-acme', slug: 'acme', name: 'Acme' } as OrgSummary;
vi.mock('../../shared/org', () => ({
  useOrg: () => org,
  useOrgHolds: (permission: string) => perms.held.has(permission),
}));
vi.mock('@tanstack/react-router', () => ({
  Link: ({ children, to, search }: { children: React.ReactNode; to: string; search?: Record<string, unknown> }) => (
    <a href={`${to}?${new URLSearchParams(Object.entries(search ?? {}).map(([k, v]) => [k, String(v)]))}`}>
      {children}
    </a>
  ),
}));

const secret = (name: string) => ({ name }) as Secret;

function renderSection({ saved = null as string | null, names = ['slack-bot', 'other'], manage = true } = {}) {
  perms.held = new Set(manage ? ['org:manage'] : []);
  api.getOrgNotificationSettings.mockResolvedValue({ slack_secret: saved });
  api.listSecrets.mockResolvedValue(names.map(secret));
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <NotificationsSection />
    </QueryClientProvider>,
  );
}

const select = () => screen.findByRole('combobox', { name: 'Slack secret' });

beforeEach(() => {
  vi.clearAllMocks();
});
afterEach(cleanup);

describe('the notifications section', () => {
  it('shows the saved secret selected, with Turn off', async () => {
    renderSection({ saved: 'other' });
    expect((await select()).textContent).toContain('other');
    expect(screen.getByRole('button', { name: 'Turn off' })).toBeTruthy();
  });

  it('preselects slack-bot when nothing is saved, without Turn off', async () => {
    renderSection();
    expect((await select()).textContent).toContain('slack-bot');
    expect(screen.queryByRole('button', { name: 'Turn off' })).toBeNull();
  });

  it('saves the picked name, and Turn off saves null', async () => {
    api.setOrgNotificationSettings.mockResolvedValue({ slack_secret: 'slack-bot' });
    renderSection();
    await select();
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(api.setOrgNotificationSettings).toHaveBeenCalledWith('org-acme', 'slack-bot'));
    fireEvent.click(await screen.findByRole('button', { name: 'Turn off' }));
    await waitFor(() => expect(api.setOrgNotificationSettings).toHaveBeenLastCalledWith('org-acme', null));
  });

  it('shows Save as busy while saving, and a second click sends nothing', async () => {
    api.setOrgNotificationSettings.mockImplementation(() => new Promise(() => {}));
    renderSection();
    await select();
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(screen.getByRole('button', { name: /Save/ }).getAttribute('aria-busy')).toBe('true'));
    const save = screen.getByRole('button', { name: /Save/ }) as HTMLButtonElement;
    expect(save.getAttribute('aria-disabled')).toBe('true');
    expect(save.disabled).toBe(false);
    fireEvent.click(save);
    expect(api.setOrgNotificationSettings).toHaveBeenCalledTimes(1);
  });

  it('shows Turn off as busy while it runs, and a second click sends nothing', async () => {
    api.setOrgNotificationSettings.mockImplementation(() => new Promise(() => {}));
    renderSection({ saved: 'other' });
    await select();
    fireEvent.click(screen.getByRole('button', { name: 'Turn off' }));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /Turn off/ }).getAttribute('aria-busy')).toBe('true'),
    );
    const off = screen.getByRole('button', { name: /Turn off/ }) as HTMLButtonElement;
    expect(off.getAttribute('aria-disabled')).toBe('true');
    expect(off.disabled).toBe(false);
    fireEvent.click(off);
    expect(api.setOrgNotificationSettings).toHaveBeenCalledTimes(1);
  });

  it('shows Test as busy while testing, and a second click sends nothing', async () => {
    api.testOrgNotifications.mockImplementation(() => new Promise(() => {}));
    renderSection();
    await select();
    fireEvent.click(screen.getByRole('button', { name: 'Test' }));
    await waitFor(() => expect(screen.getByRole('button', { name: /Test/ }).getAttribute('aria-busy')).toBe('true'));
    const test = screen.getByRole('button', { name: /Test/ }) as HTMLButtonElement;
    expect(test.getAttribute('aria-disabled')).toBe('true');
    expect(test.disabled).toBe(false);
    fireEvent.click(test);
    expect(api.testOrgNotifications).toHaveBeenCalledTimes(1);
  });

  it('raises a toast when the save fails', async () => {
    api.setOrgNotificationSettings.mockRejectedValue(
      new ApiError({ title: 'Bad', status: 400, detail: 'secret is limited to some factories' } as never),
    );
    renderSection();
    await select();
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(toast.error).toHaveBeenCalled());
  });

  it('shows the workspace when the test works', async () => {
    api.testOrgNotifications.mockResolvedValue({ ok: true, workspace: 'Acme Inc' });
    renderSection();
    await select();
    fireEvent.click(screen.getByRole('button', { name: 'Test' }));
    expect(await screen.findByText('Works with Acme Inc')).toBeTruthy();
    expect(api.testOrgNotifications).toHaveBeenCalledWith('org-acme', 'slack-bot');
  });

  it.each([
    ['slack-token-invalid', 'Slack refused this token'],
    ['slack-token-unavailable', 'Slack could not be reached. Try again.'],
    ['slack-not-set', 'Pick a secret first'],
  ])('shows %s in plain words', async (error, words) => {
    api.testOrgNotifications.mockResolvedValue({ ok: false, error });
    renderSection();
    await select();
    fireEvent.click(screen.getByRole('button', { name: 'Test' }));
    expect(await screen.findByText(words)).toBeTruthy();
  });

  it('links to New secret when there are no secrets', async () => {
    renderSection({ names: [] });
    const link = await screen.findByRole('link', { name: 'New secret' });
    expect(link.getAttribute('href')).toBe('/o/$org/secrets?new=true');
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull();
  });

  it('shows the saved value as text, with no controls, to a member', async () => {
    renderSection({ saved: 'other', manage: false });
    expect(await screen.findByText('other')).toBeTruthy();
    expect(screen.queryByRole('combobox')).toBeNull();
    for (const name of ['Test', 'Save', 'Turn off']) expect(screen.queryByRole('button', { name })).toBeNull();
  });
});

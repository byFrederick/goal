// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { keys } from '../../api/queries';
import type { AuthConfig, Member, OrgSettings, OrgSummary } from '../../api/types';
import { TooltipProvider } from '../../ui/tooltip';
import { OrgSettingsPage } from './settings-page';

// The API client, the router and the permission hooks are replaced, so each state is exact and no
// request leaves the test.
const api = vi.hoisted(() => ({
  getOrgSettings: vi.fn(),
  patchOrgSettings: vi.fn(),
  deleteOrg: vi.fn(),
  transferOwnership: vi.fn(),
}));
vi.mock('../../api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/client')>()),
  ...api,
}));

const nav = vi.hoisted(() => ({ navigate: vi.fn(), invalidate: vi.fn() }));
vi.mock('@tanstack/react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-router')>()),
  useBlocker: () => ({ status: 'idle', reset: () => undefined, proceed: () => undefined }),
  useNavigate: () => nav.navigate,
  useRouter: () => ({ invalidate: nav.invalidate }),
  Link: ({ children, to }: { children: React.ReactNode; to: string }) => <a href={to}>{children}</a>,
}));

const perms = vi.hoisted(() => ({ held: new Set<string>() }));
vi.mock('../../shared/org', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../shared/org')>()),
  useOrg: () => org,
  useOrgHolds: (permission: string) => perms.held.has(permission),
}));

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'Ac', factory_count: 3, role: 'owner' };

const settings: OrgSettings = {
  name: 'Acme',
  slug: 'acme',
  mark: 'Ac',
  slots: {
    max: 6,
    factories: [
      { factory_id: 'f-goal', name: 'goal', max: 3 },
      { factory_id: 'f-sre', name: 'sre', max: null },
      { factory_id: 'f-support', name: 'support', max: null },
    ],
  },
};

const signIn: AuthConfig = { mode: 'session', methods: ['email_code'] };

// A member as goald lists it; only the fields the page reads matter here.
const member = (id: string, name: string, role: string) =>
  ({
    member_id: `m-${id}`,
    user: { id, name, email: `${id}@example.com` },
    role,
    factory_roles: [],
    joined_at: '2026-01-01T00:00:00Z',
    last_active_at: '2026-01-02T00:00:00Z',
    you: false,
    can_change_role: true,
    can_remove: true,
  }) as unknown as Member;

const admins = [member('maya', 'Maya Ruiz', 'admin')];

function renderPage({
  held = ['org:manage', 'org:delete', 'org:transfer'],
  auth = signIn,
  members = admins,
  loading = false,
}: {
  held?: string[];
  auth?: AuthConfig;
  members?: Member[];
  loading?: boolean;
} = {}) {
  perms.held = new Set(held);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  if (!loading) client.setQueryData(keys.settings(org.id), settings);
  client.setQueryData(keys.authConfig, auth);
  client.setQueryData(keys.members(org.id), members);
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <OrgSettingsPage />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  api.patchOrgSettings.mockImplementation(async (_org: string, body: Partial<OrgSettings>) => ({
    ...settings,
    ...body,
  }));
  api.deleteOrg.mockResolvedValue({ purge_after: '2026-02-01T00:00:00Z' });
  api.transferOwnership.mockResolvedValue(org);
  api.getOrgSettings.mockResolvedValue(settings);
});

afterEach(() => {
  cleanup();
});

describe('Organization settings page', () => {
  it('shows the sections of the picture in order, with the table of contents', () => {
    renderPage();
    expect(screen.getByRole('heading', { level: 1, name: 'Organization settings' })).toBeTruthy();
    for (const name of ['General', 'Slots', 'Sign-in', 'Data', 'Danger zone']) {
      expect(screen.getByRole('heading', { level: 2, name })).toBeTruthy();
    }
    const toc = screen.getByRole('navigation', { name: 'On this page' });
    expect(
      within(toc)
        .getAllByRole('link')
        .map((a) => a.textContent),
    ).toEqual(['General', 'Slots', 'Sign-in', 'Data', 'Danger zone']);
  });

  it('shows the name, the address and the factories, each with its own slot cap', () => {
    renderPage();
    expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('Acme');
    expect((screen.getByLabelText('Address') as HTMLInputElement).value).toBe('acme');
    expect(screen.getByText('Factories')).toBeTruthy();
    for (const factory of ['goal', 'sre', 'support']) {
      expect(screen.getByLabelText(factory)).toBeTruthy();
    }
  });

  it('has no short mark, model access or secrets on the page', () => {
    renderPage();
    expect(screen.queryByLabelText('Short mark')).toBeNull();
    expect(screen.queryByText('Model access')).toBeNull();
    expect(screen.queryByText('Secrets')).toBeNull();
  });

  it('shows the loading state with the page busy and "Loading" for a screen reader', () => {
    api.getOrgSettings.mockReturnValue(new Promise(() => undefined));
    renderPage({ loading: true });
    expect(screen.getByText('Loading')).toBeTruthy();
    expect(screen.getByText('Loading').closest('[aria-busy="true"]')).toBeTruthy();
  });

  it('is read-only without org:manage, with who changes it', () => {
    renderPage({ held: [] });
    expect(screen.queryByRole('button', { name: 'Save changes' })).toBeNull();
    expect(screen.getByLabelText('Name').hasAttribute('readonly')).toBe(true);
    expect(screen.queryByRole('heading', { name: 'Danger zone' })).toBeNull();
  });

  it('shows a failed save as an alert and keeps the changes', async () => {
    api.patchOrgSettings.mockRejectedValue(new Error('network'));
    renderPage();
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Acme Robotics' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }));
    expect((await screen.findByRole('alert')).textContent).toContain('goald is not answering');
    expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('Acme Robotics');
  });
});

describe('controls the API does not have yet', () => {
  it('renders the sign-in switches, Sessions, storage, run logs and audit log disabled', () => {
    renderPage();
    for (const name of ['GitHub sign-in', 'Email code sign-in', 'Google sign-in']) {
      expect(screen.getByRole('switch', { name }).getAttribute('aria-disabled')).toBe('true');
    }
    expect(screen.getByRole('switch', { name: 'Email code sign-in' }).getAttribute('aria-checked')).toBe('true');
    expect(screen.getByRole('switch', { name: 'Google sign-in' }).getAttribute('aria-checked')).toBe('false');
    for (const name of [
      /^Set up$/,
      /^30 days$/,
      /^Use your own bucket$/,
      /^90 days$/,
      /^Open audit log$/,
      /^Export$/,
    ]) {
      expect(screen.getByRole('button', { name }).getAttribute('aria-disabled')).toBe('true');
    }
  });

  it('names the reason in a tooltip and sends nothing when pressed', async () => {
    renderPage();
    const google = screen.getByRole('switch', { name: 'Google sign-in' });
    fireEvent.focus(google);
    expect(await screen.findByText('Not available yet')).toBeTruthy();
    fireEvent.click(google);
    fireEvent.click(screen.getByRole('button', { name: /^Export$/ }));
    expect(api.patchOrgSettings).not.toHaveBeenCalled();
    expect(google.getAttribute('aria-checked')).toBe('false');
  });
});

describe('saving General and Slots', () => {
  it('sends only the changed name', async () => {
    renderPage();
    const save = screen.getByRole('button', { name: 'Save changes' });
    expect((save as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Acme Robotics' } });
    fireEvent.click(save);
    await waitFor(() => expect(api.patchOrgSettings).toHaveBeenCalledWith(org.id, { name: 'Acme Robotics' }));
  });

  it('warns that old links stop working when the address changes, and moves the page to the new one', async () => {
    renderPage();
    fireEvent.change(screen.getByLabelText('Address'), { target: { value: 'acme-co' } });
    expect(screen.getByRole('status').textContent).toContain('Links with the old address stop working.');
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }));
    await waitFor(() => expect(api.patchOrgSettings).toHaveBeenCalledWith(org.id, { slug: 'acme-co' }));
    await waitFor(() =>
      expect(nav.navigate).toHaveBeenCalledWith({
        to: '/o/$org/settings',
        params: { org: 'acme-co' },
        replace: true,
      }),
    );
  });

  it('marks an invalid slot cap at its field and does not send it', async () => {
    renderPage();
    const sre = screen.getByLabelText('sre');
    fireEvent.change(sre, { target: { value: '0' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }));
    expect(sre.getAttribute('aria-invalid')).toBe('true');
    expect(api.patchOrgSettings).not.toHaveBeenCalled();
  });
});

describe('the Danger zone', () => {
  it('deletes by typing the name inline, then leaves for /', async () => {
    renderPage();
    const confirm = screen.getByLabelText('Type Acme to confirm');
    const button = screen.getByRole('button', { name: 'Delete organization' });
    expect((button as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(confirm, { target: { value: 'acme' } });
    expect((button as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(confirm, { target: { value: 'Acme' } });
    expect((button as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(button);
    await waitFor(() => expect(api.deleteOrg).toHaveBeenCalledWith(org.id, 'Acme'));
    await waitFor(() => expect(nav.navigate).toHaveBeenCalledWith({ to: '/', replace: true }));
  });

  it('transfers ownership to the admin chosen in the row, after the name is typed', async () => {
    renderPage();
    fireEvent.click(screen.getByRole('button', { name: 'Transfer' }));
    const dialog = await screen.findByRole('dialog', { name: 'Transfer ownership of Acme' });
    expect(within(dialog).getByText('Maya Ruiz')).toBeTruthy();
    const confirm = within(dialog).getByLabelText('Type Acme to confirm');
    fireEvent.change(confirm, { target: { value: 'Acme' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Transfer ownership' }));
    await waitFor(() => expect(api.transferOwnership).toHaveBeenCalledWith(org.id, 'maya', 'Acme'));
  });

  it('says there is no admin to hand it to, and disables Transfer', () => {
    renderPage({ members: [member('ana', 'Ana Lee', 'owner')] });
    expect(screen.getByText('Make someone an admin first.')).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Transfer' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('leaves out Delete and Transfer without the owner permissions', () => {
    renderPage({ held: ['org:manage'] });
    expect(screen.queryByRole('heading', { name: 'Danger zone' })).toBeNull();
  });
});

// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../api/client';
import type { OrgPolicyView, OrgSummary } from '../../api/types';
import { TooltipProvider } from '../../ui/tooltip';
import { ModelsPage } from './models-page';

// The policy API and the permission hook are replaced, so each state is exact and no request leaves the
// test. The fixture is the acme policy models.jpg draws.
const api = vi.hoisted(() => ({ getOrgPolicy: vi.fn(), putOrgPolicy: vi.fn() }));
vi.mock('../../api/policy', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/policy')>()),
  ...api,
}));

// The top bar is the app's shell: here the page's actions render where the page puts them, so the test
// can reach Save changes.
vi.mock('../../app/top-bar', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../app/top-bar')>()),
  TopBarSlot: ({ actions }: { actions?: React.ReactNode }) => <div>{actions}</div>,
}));

const perms = vi.hoisted(() => ({ held: new Set<string>() }));
vi.mock('../../shared/org', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../shared/org')>()),
  useOrg: () => org,
  useOrgHolds: (permission: string) => perms.held.has(permission),
}));

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'Ac', factory_count: 3, role: 'owner' };

const GIB = 1024 ** 3;

/** The acme policy as models.jpg draws it, at revision 3. */
function acme(overrides: Partial<OrgPolicyView['policy']> = {}): OrgPolicyView {
  return {
    policy: {
      allowed_models: ['claude-opus-5-5', 'claude-sonnet-5-5', 'claude-haiku-5-5', 'claude-fable-5-1'],
      default_model: 'claude-opus-5-5',
      denied_commands: ['Bash(terraform apply:*)', 'Bash(git push * main:*)'],
      tools: {
        write_outside_branch: 'ask',
        merge_pull_request: 'review_approval',
        read_web: 'allowed',
        use_credentials: 'attached_only',
      },
      box_time_limit_minutes: 180,
      ...overrides,
    },
    revision: 3,
    models: [
      { id: 'claude-opus-5-5', label: 'Claude Opus 5.5', used_by: [] },
      {
        id: 'claude-sonnet-5-5',
        label: 'Claude Sonnet 5.5',
        used_by: [{ factory_id: 'f-support', name: 'support', agents: ['responder'] }],
      },
      {
        id: 'claude-haiku-5-5',
        label: 'Claude Haiku 5.5',
        used_by: [{ factory_id: 'f-software', name: 'software', agents: ['frontend-engineer'] }],
      },
      { id: 'claude-fable-5-1', label: 'Claude Fable 5.1', used_by: [] },
    ],
    box_sizes: [
      { size: 'small', vcpu: 2, memory_bytes: 4 * GIB, factories: [{ factory_id: 'f-support', name: 'support' }] },
      { size: 'medium', vcpu: 4, memory_bytes: 8 * GIB, factories: [] },
      { size: 'large', vcpu: 8, memory_bytes: 16 * GIB, factories: [{ factory_id: 'f-software', name: 'software' }] },
    ],
    egress_addresses: ['192.0.2.0/24', '2001:db8::/32'],
  };
}

const load = (view: OrgPolicyView = acme()) => ({ view, etag: '"3"' });

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <ModelsPage />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

const save = () => screen.getByRole('button', { name: 'Save changes' });

beforeEach(() => {
  perms.held = new Set(['org:manage']);
  api.getOrgPolicy.mockResolvedValue(load());
  api.putOrgPolicy.mockReset();
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('Models and policies', () => {
  it('shows the loading state while the policy loads', () => {
    api.getOrgPolicy.mockReturnValue(new Promise(() => undefined));
    renderPage();

    expect(screen.getByRole('heading', { name: 'Models and policies' })).toBeTruthy();
    expect(screen.getByRole('status').textContent).toBe('Loading models and policies');
  });

  it('shows the error with Try again, which reads the policy once more', async () => {
    api.getOrgPolicy.mockRejectedValueOnce(new Error('network'));
    renderPage();

    const alert = await screen.findByRole('alert');
    expect(within(alert).getByText(/Couldn’t load models and policies/)).toBeTruthy();
    api.getOrgPolicy.mockResolvedValueOnce(load());
    fireEvent.click(within(alert).getByRole('button', { name: 'Try again' }));

    expect(await screen.findByRole('heading', { name: 'Models agents may use' })).toBeTruthy();
  });

  it('shows the empty state when the install has no models', async () => {
    api.getOrgPolicy.mockResolvedValue(load({ ...acme(), models: [] } as OrgPolicyView));
    renderPage();

    expect(await screen.findByText('No models to choose from')).toBeTruthy();
  });

  it('shows the ready state as the picture draws it', async () => {
    renderPage();

    await screen.findByRole('heading', { name: 'Models agents may use' });
    expect(
      screen.getByText('Defaults for every factory in Goal. A factory or an agent can choose less, never more.'),
    ).toBeTruthy();
    for (const name of [
      'Models agents may use',
      'Tool policy',
      'Commands no agent may run',
      'Box sizes',
      'Where agents connect from',
    ]) {
      expect(screen.getByRole('heading', { name })).toBeTruthy();
    }
    expect(screen.getByRole('switch', { name: 'Allow Claude Opus 5.5' }).getAttribute('aria-checked')).toBe('true');
    expect(screen.getByText('All agents unless they choose another')).toBeTruthy();
    expect(screen.getByText("Used by support's responder")).toBeTruthy();
    expect(screen.getByRole('combobox', { name: /Write outside the item/ })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Remove terraform apply' })).toBeTruthy();
    expect((screen.getByRole('textbox', { name: 'Hours before a box stops' }) as HTMLInputElement).value).toBe('3');
    expect(screen.getByText('192.0.2.0/24')).toBeTruthy();
  });

  it('keeps a routers row and another harness not available yet', async () => {
    renderPage();

    const router = await screen.findByRole('button', { name: 'Add a router' });
    expect(router.getAttribute('aria-disabled')).toBe('true');
    fireEvent.focus(router);
    expect(await screen.findByText('Not available yet')).toBeTruthy();

    const other = screen.getByRole('radio', { name: 'Another harness' });
    expect(other.getAttribute('aria-disabled')).toBe('true');
    expect((screen.getByRole('radio', { name: 'Claude Code' }) as HTMLInputElement).checked).toBe(true);
  });

  it('keeps Save changes off until the form differs from the policy as loaded', async () => {
    renderPage();
    await screen.findByRole('heading', { name: 'Models agents may use' });
    expect(save().hasAttribute('disabled')).toBe(true);

    fireEvent.click(screen.getByRole('switch', { name: 'Allow Claude Sonnet 5.5' }));
    expect(save().hasAttribute('disabled')).toBe(false);
  });

  it('moves the default to the next model still on when the default is turned off', async () => {
    renderPage();
    await screen.findByRole('heading', { name: 'Models agents may use' });

    fireEvent.click(screen.getByRole('switch', { name: 'Allow Claude Opus 5.5' }));

    const sonnet = screen.getByRole('switch', { name: 'Allow Claude Sonnet 5.5' }).closest('li') as HTMLElement;
    expect(within(sonnet).getByText('Default')).toBeTruthy();
  });

  it('saves the edited policy on the revision it was loaded with', async () => {
    api.putOrgPolicy.mockResolvedValue({ view: acme(), etag: '"4"' });
    renderPage();
    await screen.findByRole('heading', { name: 'Models agents may use' });

    fireEvent.click(screen.getByRole('switch', { name: 'Allow Claude Fable 5.1' }));
    fireEvent.click(save());

    await waitFor(() => expect(api.putOrgPolicy).toHaveBeenCalledTimes(1));
    const [orgId, body, etag] = api.putOrgPolicy.mock.calls[0] as [string, { allowed_models: string[] }, string];
    expect(orgId).toBe('org-1');
    expect(etag).toBe('"3"');
    expect(body.allowed_models).not.toContain('claude-fable-5-1');
  });

  it('keeps the edits and asks for a save again when the policy changed while editing', async () => {
    api.putOrgPolicy.mockRejectedValue(
      new ApiError({ status: 409, title: 'Conflict', code: 'policy-conflict', detail: 'stale' }),
    );
    renderPage();
    await screen.findByRole('heading', { name: 'Models agents may use' });

    fireEvent.click(screen.getByRole('switch', { name: 'Allow Claude Sonnet 5.5' }));
    fireEvent.click(save());

    expect(await screen.findByText(/The policy changed while you were editing/)).toBeTruthy();
    expect(screen.getByRole('switch', { name: 'Allow Claude Sonnet 5.5' }).getAttribute('aria-checked')).toBe('false');
  });

  it('shows the detail of a save goald refuses with 422', async () => {
    api.putOrgPolicy.mockRejectedValue(
      new ApiError({
        status: 422,
        title: 'Unprocessable',
        code: 'policy-invalid',
        detail: 'a denied command is blank',
      }),
    );
    renderPage();
    await screen.findByRole('heading', { name: 'Models agents may use' });

    fireEvent.click(screen.getByRole('switch', { name: 'Allow Claude Sonnet 5.5' }));
    fireEvent.click(save());

    expect(await screen.findByText('Not saved: a denied command is blank')).toBeTruthy();
  });

  it('keeps the box time limit in its minutes when only another setting changes', async () => {
    api.getOrgPolicy.mockResolvedValue(load(acme({ box_time_limit_minutes: 80 })));
    api.putOrgPolicy.mockResolvedValue({ view: acme({ box_time_limit_minutes: 80 }), etag: '"4"' });
    renderPage();
    await screen.findByRole('heading', { name: 'Box sizes' });

    fireEvent.change(screen.getByRole('combobox', { name: /Read the web/ }), { target: { value: 'ask' } });
    fireEvent.click(save());

    await waitFor(() => expect(api.putOrgPolicy).toHaveBeenCalledTimes(1));
    const body = api.putOrgPolicy.mock.calls[0]?.[1] as { box_time_limit_minutes: number };
    expect(body.box_time_limit_minutes).toBe(80);
  });

  it('saves a box time limit typed in hours as minutes', async () => {
    api.putOrgPolicy.mockResolvedValue({ view: acme(), etag: '"4"' });
    renderPage();
    await screen.findByRole('heading', { name: 'Box sizes' });

    fireEvent.change(screen.getByRole('textbox', { name: 'Hours before a box stops' }), { target: { value: '1.5' } });
    fireEvent.click(save());

    await waitFor(() => expect(api.putOrgPolicy).toHaveBeenCalledTimes(1));
    const body = api.putOrgPolicy.mock.calls[0]?.[1] as { box_time_limit_minutes: number };
    expect(body.box_time_limit_minutes).toBe(90);
  });

  it('adds a denied command as a chip, and removes one', async () => {
    renderPage();
    await screen.findByRole('heading', { name: 'Commands no agent may run' });

    fireEvent.click(screen.getByRole('button', { name: '+ Add a command' }));
    const field = screen.getByRole('textbox', { name: 'Command' });
    expect((screen.getByRole('button', { name: 'Add' }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(field, { target: { value: 'gh release' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add' }));

    expect(screen.getByText('gh release')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Remove gh release' }));
    expect(screen.queryByText('gh release')).toBeNull();
  });

  it('copies the egress addresses with Copy all', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
    renderPage();
    await screen.findByRole('heading', { name: 'Where agents connect from' });

    fireEvent.click(screen.getByRole('button', { name: 'Copy all' }));

    await waitFor(() => expect(writeText).toHaveBeenCalledWith('192.0.2.0/24\n2001:db8::/32'));
  });

  it('shows the policy read-only without org:manage and takes no change', async () => {
    perms.held = new Set();
    renderPage();
    await screen.findByRole('heading', { name: 'Models agents may use' });

    expect(screen.getByText('View only. Owners and admins change these policies.')).toBeTruthy();
    expect(screen.getByRole('switch', { name: 'Allow Claude Opus 5.5' }).getAttribute('aria-disabled')).toBe('true');
    expect((screen.getByRole('combobox', { name: /Read the web/ }) as HTMLSelectElement).disabled).toBe(true);
    expect(screen.queryByRole('button', { name: '+ Add a command' })).toBeNull();
    expect(save().hasAttribute('disabled')).toBe(true);
  });
});

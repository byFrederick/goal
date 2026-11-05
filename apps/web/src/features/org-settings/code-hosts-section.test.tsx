// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../api/client';
import type { CodeHost, OrgSummary } from '../../api/types';
import { TooltipProvider } from '../../ui/tooltip';
import { CodeHostsSection } from './code-hosts-section';

const api = vi.hoisted(() => ({ getCodeHosts: vi.fn(), setCodeHosts: vi.fn() }));
vi.mock('../../api/code-hosts', () => api);

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock('sonner', () => ({ toast }));

const perms = vi.hoisted(() => ({ held: new Set<string>() }));
const org = { id: 'org-acme', slug: 'acme', name: 'Acme' } as OrgSummary;
vi.mock('../../shared/org', () => ({
  useOrg: () => org,
  useOrgHolds: (permission: string) => perms.held.has(permission),
}));

const builtIn: CodeHost[] = [
  { host: 'github.com', kind: 'github', api_base: 'https://api.github.com', built_in: true },
  { host: 'gitlab.com', kind: 'gitlab', api_base: 'https://gitlab.com/api/v4', built_in: true },
];
const ghe: CodeHost = {
  host: 'ghe.example.com',
  kind: 'github',
  api_base: 'https://ghe.example.com/api/v3',
  built_in: false,
};

function renderSection({ added = [] as CodeHost[], edit = true } = {}) {
  perms.held = new Set(edit ? ['org:code_hosts'] : []);
  api.getCodeHosts.mockResolvedValue({ hosts: [...builtIn, ...added] });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <CodeHostsSection />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

const problem = (status: number, code: string, detail: string) =>
  new ApiError({ type: 'about:blank', title: 'Bad request', status, code, detail } as never);

beforeEach(() => {
  vi.clearAllMocks();
  // jsdom has no layout: Radix Select scrolls the picked option into view and checks pointer capture.
  Element.prototype.scrollIntoView = () => undefined;
  Element.prototype.hasPointerCapture = () => false;
});
afterEach(cleanup);

describe('the code hosts section', () => {
  it('lists built-in hosts first, then added ones', async () => {
    renderSection({ added: [ghe] });
    await screen.findByText('ghe.example.com');
    const names = screen.getAllByText(/\.(com)$/).map((n) => n.textContent);
    expect(names).toEqual(['github.com', 'gitlab.com', 'ghe.example.com']);
    expect(screen.getAllByText('Built in')).toHaveLength(2);
    expect(screen.getByText(/GitHub · https:\/\/ghe\.example\.com\/api\/v3/)).toBeTruthy();
    expect(screen.getByRole('button', { name: 'About Code hosts' })).toBeTruthy();
  });

  it('adds a host with its kind', async () => {
    renderSection({ added: [ghe] });
    await screen.findByText('ghe.example.com');
    const added = {
      host: 'gl.example.com',
      kind: 'gitlab' as const,
      api_base: 'https://gl.example.com/api/v4',
      built_in: false,
    };
    api.setCodeHosts.mockResolvedValue({ hosts: [...builtIn, ghe, added] });
    fireEvent.change(screen.getByLabelText('Add a host'), { target: { value: 'GL.example.com' } });
    fireEvent.keyDown(screen.getByRole('combobox', { name: 'Kind' }), { key: 'ArrowDown' });
    fireEvent.click(await screen.findByRole('option', { name: 'GitLab' }));
    fireEvent.click(screen.getByRole('button', { name: 'Add' }));
    await screen.findByText('gl.example.com');
    expect(api.setCodeHosts).toHaveBeenCalledWith('org-acme', [
      { host: 'ghe.example.com', kind: 'github' },
      { host: 'gl.example.com', kind: 'gitlab' },
    ]);
    expect((screen.getByLabelText('Add a host') as HTMLInputElement).value).toBe('');
  });

  it('removes an added host', async () => {
    renderSection({ added: [ghe] });
    await screen.findByText('ghe.example.com');
    api.setCodeHosts.mockResolvedValue({ hosts: builtIn });
    fireEvent.click(screen.getByRole('button', { name: 'Remove ghe.example.com' }));
    await waitFor(() => expect(screen.queryByText('ghe.example.com')).toBeNull());
    expect(api.setCodeHosts).toHaveBeenCalledWith('org-acme', []);
  });

  it('shows why a host is refused', async () => {
    renderSection({ added: [ghe] });
    await screen.findByText('ghe.example.com');
    api.setCodeHosts.mockRejectedValue(problem(400, 'code-host-invalid', 'host must be a public DNS name'));
    fireEvent.change(screen.getByLabelText('Add a host'), { target: { value: 'localhost' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add' }));
    expect(await screen.findByText('host must be a public DNS name')).toBeTruthy();
    expect(screen.getByText('ghe.example.com')).toBeTruthy();

    api.setCodeHosts.mockRejectedValue(problem(400, 'code-host-built-in', 'x'));
    fireEvent.change(screen.getByLabelText('Add a host'), { target: { value: 'github.com' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add' }));
    expect(await screen.findByText('github.com and gitlab.com are always listed')).toBeTruthy();

    api.setCodeHosts.mockRejectedValue(new ApiError({ type: 'about:blank', title: 'Boom', status: 500 } as never));
    fireEvent.click(screen.getByRole('button', { name: 'Add' }));
    await waitFor(() => expect(toast.error).toHaveBeenCalled());
  });

  it('shows the list read only without org:code_hosts', async () => {
    renderSection({ added: [ghe], edit: false });
    await screen.findByText('ghe.example.com');
    expect(screen.queryByLabelText('Add a host')).toBeNull();
    expect(screen.queryByRole('button', { name: /^Remove/ })).toBeNull();
  });
});

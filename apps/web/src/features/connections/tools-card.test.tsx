// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { updateConnection } from '../../api/connections';
import type { Connection, OrgSummary } from '../../api/types';
import { connectionFixture } from '../../test/connections-route';
import { ToolsCard } from './tools-card';

// The card saves through updateConnection; a test answers it, or fails it for the error state.
vi.mock(import('../../api/connections'), async (original) => ({
  ...(await original()),
  updateConnection: vi.fn(),
}));

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: 'owner' };

// The card reads the organization it is in; the test gives it one rather than a router.
vi.mock(import('../../shared/org'), async (original) => ({
  ...(await original()),
  useOrg: () => org,
}));

const tools: Connection['tools'] = [
  { name: 'create_comment', mode: 'ask' },
  { name: 'update_issue_status', mode: 'allow' },
  { name: 'create_issue', mode: 'off' },
];

function renderCard(connection: Connection, manage = true) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <ToolsCard connection={connection} manage={manage} />
    </QueryClientProvider>,
  );
}

/** The three-way group of one tool, by the tool's name. */
const group = (tool: string) => screen.getByRole('group', { name: tool });

afterEach(() => {
  cleanup();
  vi.mocked(updateConnection).mockReset();
});

describe('the tools of an MCP connection', () => {
  it('lists every tool with its mode selected, and how many were found', () => {
    renderCard(connectionFixture({ name: 'linear', kind: 'mcp-server', tools }));

    const heading = screen.getByRole('heading', { level: 2, name: /Tools agents may use\s*3 found/ });
    expect(heading.className).toContain('text-heading');
    expect(heading.className).not.toContain('text-lg');
    const selected = (tool: string) =>
      within(group(tool)).getByRole('radio', { checked: true }).parentElement?.textContent;
    expect(selected('create_comment')).toBe('Ask first');
    expect(selected('update_issue_status')).toBe('Allow');
    expect(selected('create_issue')).toBe('Off');
    expect(within(group('create_comment')).getAllByRole('radio')).toHaveLength(3);
  });

  it('saves the whole list with the one tool changed, and reads the connection again', async () => {
    vi.mocked(updateConnection).mockResolvedValue(connectionFixture({ name: 'linear', kind: 'mcp-server', tools }));
    renderCard(connectionFixture({ name: 'linear', kind: 'mcp-server', tools }));

    fireEvent.click(within(group('create_issue')).getByRole('radio', { name: 'Ask first' }));

    await waitFor(() => expect(updateConnection).toHaveBeenCalledTimes(1));
    expect(updateConnection).toHaveBeenCalledWith('org-1', 'linear', {
      tools: [
        { name: 'create_comment', mode: 'ask' },
        { name: 'update_issue_status', mode: 'allow' },
        { name: 'create_issue', mode: 'ask' },
      ],
    });
  });

  it('shows the three settings disabled to a caller without connection:manage', () => {
    renderCard(connectionFixture({ name: 'linear', kind: 'mcp-server', tools }), false);

    const radios = within(group('create_comment')).getAllByRole('radio');
    expect(radios.map((b) => (b as HTMLInputElement).disabled)).toEqual([true, true, true]);
    expect(within(group('create_comment')).getByRole('radio', { checked: true }).parentElement?.textContent).toBe(
      'Ask first',
    );
  });

  it('says to test the connection when it has no tools yet', () => {
    renderCard(connectionFixture({ name: 'linear', kind: 'mcp-server', tools: [] }));

    expect(screen.getByText('0 found')).toBeTruthy();
    expect(screen.getByText(/Test the connection to list the tools/)).toBeTruthy();
    expect(screen.queryByRole('group')).toBeNull();
  });

  it('shows the refusal with Try again, which sends the same list again', async () => {
    vi.mocked(updateConnection)
      .mockRejectedValueOnce(new Error('goald is not answering'))
      .mockResolvedValueOnce(connectionFixture({ name: 'linear', kind: 'mcp-server', tools }));
    renderCard(connectionFixture({ name: 'linear', kind: 'mcp-server', tools }));

    fireEvent.click(within(group('create_issue')).getByRole('radio', { name: 'Allow' }));
    const alert = await screen.findByRole('alert');
    expect(within(alert).getByText(/goald is not answering/)).toBeTruthy();

    fireEvent.click(within(alert).getByRole('button', { name: 'Try again' }));
    await waitFor(() => expect(updateConnection).toHaveBeenCalledTimes(2));
    expect(vi.mocked(updateConnection).mock.calls[1]?.[2]).toEqual(vi.mocked(updateConnection).mock.calls[0]?.[2]);
  });
});

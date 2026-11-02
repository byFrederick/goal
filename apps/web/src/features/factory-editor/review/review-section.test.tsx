// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { History } from './review-section';

const versions = vi.hoisted(() => ({
  state: { isPending: true, isError: false, data: undefined as unknown, refetch: vi.fn() },
}));

vi.mock('@tanstack/react-query', async (orig) => ({
  ...(await orig<typeof import('@tanstack/react-query')>()),
  useQuery: () => versions.state,
}));
vi.mock('../search-link', () => ({
  SectionLink: ({ children }: { children: ReactNode }) => <a href="/versions">{children}</a>,
  useSetSearch: () => vi.fn(),
}));
vi.mock('./use-review', () => ({ useEditorFacts: () => ({ org: 'o', factory: 'f', hasDraft: false }) }));
vi.mock('./version-history', () => ({
  DraftRow: () => null,
  draftDetail: () => '',
  RestoreDialog: () => null,
  VersionRow: ({ version }: { version: { number: number } }) => <li>Row v{version.number}</li>,
}));

afterEach(cleanup);

function renderHistory() {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <History compare={undefined} hasDraft={false} next={4} draftUpdated={undefined} />
    </QueryClientProvider>,
  );
}

describe('Versions card', () => {
  it('shows a skeleton while the versions load, with the link under it', () => {
    versions.state = { isPending: true, isError: false, data: undefined, refetch: vi.fn() };
    renderHistory();
    expect(screen.getByText('Loading').closest('[aria-busy="true"]')).not.toBeNull();
    expect(screen.getByRole('link', { name: 'All versions' })).toBeTruthy();
  });

  it('says what failed and retries', () => {
    const refetch = vi.fn();
    versions.state = { isPending: false, isError: true, data: undefined, refetch };
    renderHistory();
    expect(screen.getByText('The versions could not be loaded.')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(refetch).toHaveBeenCalledOnce();
    expect(screen.getByRole('link', { name: 'All versions' })).toBeTruthy();
  });

  it('lists the newest four versions and links to all of them', () => {
    const list = [6, 5, 4, 3, 2, 1].map((number) => ({ id: `v${number}`, number }));
    versions.state = { isPending: false, isError: false, data: { versions: list }, refetch: vi.fn() };
    renderHistory();
    expect(screen.getAllByText(/^Row v/).map((e) => e.textContent)).toEqual(['Row v6', 'Row v5', 'Row v4', 'Row v3']);
    expect(screen.getByRole('link', { name: 'All 6 versions' })).toBeTruthy();
  });
});

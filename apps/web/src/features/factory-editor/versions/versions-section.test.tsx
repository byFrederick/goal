// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { VersionPage } from '../../../api/types';
import { TooltipProvider } from '../../../ui/tooltip';
import { VersionsSection } from './versions-section';

const listVersions = vi.hoisted(() => vi.fn());
vi.mock('../../../api/definitions', () => ({ listVersions }));
vi.mock('../draft/use-draft', () => ({ useViewOnly: () => false }));
vi.mock('../review/use-review', () => ({
  useEditorFacts: () => ({ org: 'org-1', factory: 'factory-1', hasDraft: false, next: 3, draft: undefined }),
}));
vi.mock('../review/version-history', () => ({
  DraftRow: () => null,
  draftDetail: () => '',
  RestoreDialog: () => null,
  VersionRow: ({ version }: { version: { number: number; message: string } }) => (
    <li>{`v${version.number} ${version.message}`}</li>
  ),
}));
vi.mock('../frame/section-shell', () => ({
  SectionShell: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));
vi.mock('../review/changes-card', () => ({ ChangesCard: () => null }));

const page = {
  versions: [
    { id: 'v2', number: 2, message: 'Tighten review', current: true },
    { id: 'v1', number: 1, message: 'First line', current: false },
  ],
  next_before: 1,
} as unknown as VersionPage;

function renderSection() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const { container } = render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <VersionsSection place="definition" search={{}} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return container;
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('VersionsSection', () => {
  it('shows a busy skeleton while loading, then the rows and older versions', async () => {
    let resolve: (p: VersionPage) => void = () => {};
    listVersions.mockReturnValue(new Promise<VersionPage>((r) => (resolve = r)));
    const container = renderSection();

    expect(container.querySelector('[aria-busy="true"]')).not.toBeNull();
    expect(screen.getByText('Loading')).toBeTruthy();

    resolve(page);
    expect(await screen.findByText('v2 Tighten review')).toBeTruthy();
    expect(screen.getByText('v1 First line')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Show older versions' })).toBeTruthy();
    expect(container.querySelector('[aria-busy="true"]')).toBeNull();
  });
});

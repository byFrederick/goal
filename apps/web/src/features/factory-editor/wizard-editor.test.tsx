// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, render, screen } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { WizardEditor } from './wizard-editor';

const published = vi.hoisted(() => ({
  id: 'f1',
  name: 'acme-support',
  title: 'Acme Support Desk',
  current: { number: 4 },
}));

vi.mock('../../shared/org', () => ({ useOrg: () => ({ id: 'o1', slug: 'acme', name: 'Acme' }) }));
vi.mock('../../api/queries', () => {
  const query = (key: string, data: unknown) => () => ({ queryKey: [key], queryFn: () => Promise.resolve(data) });
  return {
    factoryQuery: query('factory', published),
    draftQuery: query('draft', null),
    templatesQuery: query('templates', []),
    factoriesQuery: query('factories', []),
    keys: { factories: () => ['factories'], overviews: () => ['overviews'] },
  };
});
vi.mock('./draft/use-draft', () => ({
  useDraftSession: () => ({}),
  useDraft: (select: (s: unknown) => unknown) => select({ status: 'saved' }),
  useModel: () => ({ basics: { name: '' }, line: {}, agents: {}, connections: [] }),
}));
vi.mock('./definition-page', () => ({ SectionView: () => null }));
vi.mock('./frame/editor-frame', () => ({ useUndoKeys: () => {} }));
vi.mock('./frame/conflict-dialog', () => ({
  ConflictDialog: () => null,
  DiscardedDialog: () => null,
  FileConflictDialog: () => null,
}));
vi.mock('./frame/unsaved-guard', () => ({ UnsavedGuard: () => null }));
vi.mock('./frame/wizard-frame', () => ({
  DraftSaved: () => null,
  WizardFooter: () => null,
  WizardHeader: () => null,
  WizardViewToggle: () => null,
}));
vi.mock('./frame/wizard-steps', () => ({ WizardSteps: () => null }));
vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => vi.fn(),
  Link: ({ children }: { children?: ReactNode }) => <a href="/x">{children}</a>,
}));

afterEach(cleanup);

describe('WizardEditor, a factory already published', () => {
  it('names the factory by its title, not its name', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <WizardEditor step="basics" factory="acme-support" search={{}} />
      </QueryClientProvider>,
    );
    expect(await screen.findByText('Acme Support Desk is published')).toBeTruthy();
    expect(screen.queryByText(/acme-support is published/)).toBeNull();
  });
});

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
  current: { number: 4 } as { number: number } | undefined,
}));
const detail = vi.hoisted(() => ({
  load: (): Promise<unknown> => Promise.resolve(null),
}));

vi.mock('../../shared/org', () => ({ useOrg: () => ({ id: 'o1', slug: 'acme', name: 'Acme' }) }));
vi.mock('../../api/queries', () => {
  const query = (key: string, data: unknown) => () => ({ queryKey: [key], queryFn: () => Promise.resolve(data) });
  return {
    factoryQuery: () => ({ queryKey: ['factory'], queryFn: () => detail.load() }),
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
  WizardFooter: ({ start, end }: { start?: ReactNode; end?: ReactNode }) => (
    <footer>
      {start}
      {end}
    </footer>
  ),
  WizardHeader: ({ title, chip }: { title: string; chip?: ReactNode }) => (
    <header>
      {title}
      {chip}
    </header>
  ),
  WizardViewToggle: () => null,
}));
vi.mock('./frame/wizard-steps', () => ({ WizardSteps: () => <nav>New factory steps</nav> }));
vi.mock('../../ui/confirm-dialog', () => ({ ConfirmDialog: () => null }));
vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => vi.fn(),
  Link: ({ children }: { children?: ReactNode }) => <a href="/x">{children}</a>,
}));

afterEach(cleanup);

describe('WizardEditor, a factory already published', () => {
  it('names the factory by its title, not its name', async () => {
    detail.load = () => Promise.resolve(published);
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

describe('WizardEditor while the factory detail loads', () => {
  const mount = () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <WizardEditor step="basics" factory="acme-support" search={{}} />
      </QueryClientProvider>,
    );
  };

  it('draws no draft chrome until the detail answers, then the published state', async () => {
    let resolve: (v: unknown) => void = () => {};
    detail.load = () => new Promise((r) => (resolve = r));
    mount();
    expect(screen.getByText('Loading')).toBeTruthy();
    expect(screen.queryByText(/Delete this draft factory/)).toBeNull();
    expect(screen.queryByText('Draft')).toBeNull();
    expect(screen.queryByText(/Continue to/)).toBeNull();
    expect(screen.queryByText('New factory steps')).toBeNull();
    resolve({ ...published, current: { number: 4 } });
    expect(await screen.findByText('Acme Support Desk is published')).toBeTruthy();
  });

  it('shows the draft wizard when the detail has no published version', async () => {
    let resolve: (v: unknown) => void = () => {};
    detail.load = () => new Promise((r) => (resolve = r));
    mount();
    expect(screen.queryByText('Draft')).toBeNull();
    resolve({ ...published, current: undefined });
    expect(await screen.findByText('Draft')).toBeTruthy();
    expect(screen.getByText(/Continue to/)).toBeTruthy();
    expect(screen.getByText(/Delete this draft factory/)).toBeTruthy();
  });
});

// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Validation } from '../../../api/types';
import { TooltipProvider } from '../../../ui/tooltip';
import { PublishCard } from './publish-card';

// The card reads the draft session and the editor facts; the test sets them and watches the card.
const state = vi.hoisted(() => ({ hasDraft: true }));
const closeDraft = vi.hoisted(() =>
  vi.fn(() => {
    state.hasDraft = false;
  }),
);
const toast = vi.hoisted(() => vi.fn());
const publishDraft = vi.hoisted(() => vi.fn(async () => ({ number: 5 })));
const startDraftKeptTests = vi.hoisted(() => vi.fn());
const getDraftKeptTests = vi.hoisted(() => vi.fn());

vi.mock('sonner', () => ({ toast }));
vi.mock('../../../api/definitions', () => ({ publishDraft }));
vi.mock('../../../api/client', async (original) => ({
  ...(await original<typeof import('../../../api/client')>()),
  startDraftKeptTests,
  getDraftKeptTests,
}));
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn() }));
vi.mock('../../../shared/org', () => ({ useOrg: () => ({ id: 'org-1', slug: 'acme' }) }));
vi.mock('../draft/use-draft', () => ({
  useDraftSession: () => ({
    flush: async () => undefined,
    hasUnsaved: () => false,
    getSnapshot: () => ({ revision: 3 }),
    closeDraft,
  }),
  useModel: () => ({ line: { stations: [] } }),
}));
vi.mock('./use-review', () => ({
  useComparison: () => ({ data: { changes: [] } }),
  useEditorFacts: () => ({
    org: 'org-1',
    factory: 'factory-1',
    hasDraft: state.hasDraft,
    current: { id: 'v4', number: 4 },
    next: 5,
    detail: { name: 'Line' },
  }),
}));

const validation = { problems: [], counts: {}, checks: [] } as unknown as Validation;

function renderCard() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <PublishCard place="definition" validation={validation} moves={{}} onMoves={() => {}} needs={[]} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

const kept = (id: string, number: number, result: string | null, extra: object = {}) => ({
  test: {
    id,
    item: { id: `i${number}`, number, title: `Item ${number}` },
    station: 'build',
    kept_by: 'user:u1',
    kept_at: '2026-10-26T10:00:00Z',
    last_result: null,
  },
  result,
  ...extra,
});

beforeEach(() => {
  state.hasDraft = true;
  startDraftKeptTests.mockResolvedValue({ state: 'done', tests: [] });
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('PublishCard', () => {
  it('shows a check and the published version once the draft publishes', async () => {
    renderCard();

    fireEvent.click(screen.getByRole('button', { name: 'Publish v5' }));

    expect(await screen.findByRole('status')).toBeTruthy();
    const published = screen.getByRole('button', { name: 'Published' });
    expect(published.getAttribute('aria-disabled')).toBe('true');
    expect(screen.queryByRole('button', { name: 'Publish v5' })).toBeNull();
    expect(toast).toHaveBeenCalledWith('Line v5 published');
    expect(screen.getByText('v5 is published and running.')).toBeTruthy();
    expect(closeDraft).toHaveBeenCalledOnce();
    await waitFor(() => expect(publishDraft).toHaveBeenCalledOnce());
  });

  it('says what runs when there is no draft and nothing was just published', () => {
    state.hasDraft = false;
    renderCard();

    expect(screen.getByText('No draft. v4 is running.')).toBeTruthy();
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('starts the kept tests once, lists their results as they arrive and never blocks Publish on a failure', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      startDraftKeptTests.mockResolvedValue({
        state: 'trying',
        tests: [kept('a', 147, 'running'), kept('b', 150, 'running')],
      });
      getDraftKeptTests.mockResolvedValue({
        state: 'done',
        tests: [kept('a', 147, 'passed'), kept('b', 150, 'failed', { reason: 'The retry cap was not read.' })],
      });
      renderCard();

      const list = await screen.findByRole('list');
      expect(within(list).getAllByText('Running')).toHaveLength(2);
      expect(startDraftKeptTests).toHaveBeenCalledOnce();

      await vi.advanceTimersByTimeAsync(3100);
      expect(await within(list).findByText('Passed')).toBeTruthy();
      expect(within(list).getByText('Failed')).toBeTruthy();
      expect(screen.getByText('The retry cap was not read.')).toBeTruthy();
      expect(startDraftKeptTests).toHaveBeenCalledOnce();

      const publish = screen.getByRole('button', { name: 'Publish v5' });
      expect(publish.getAttribute('aria-disabled')).toBeNull();
      fireEvent.click(publish);
      await waitFor(() => expect(publishDraft).toHaveBeenCalledOnce());

      // Done: no more reads.
      const reads = getDraftKeptTests.mock.calls.length;
      await vi.advanceTimersByTimeAsync(9000);
      expect(getDraftKeptTests.mock.calls.length).toBe(reads);
    } finally {
      vi.useRealTimers();
    }
  });

  it('warns with the reason when a test could not be tried', async () => {
    startDraftKeptTests.mockResolvedValue({
      state: 'done',
      tests: [kept('a', 147, 'error', { error: 'The agent needs secrets.' })],
    });
    getDraftKeptTests.mockResolvedValue({
      state: 'done',
      tests: [kept('a', 147, 'error', { error: 'The agent needs secrets.' })],
    });
    renderCard();

    expect(await screen.findByText('The agent needs secrets.')).toBeTruthy();
    expect(screen.getByText('Not tried')).toBeTruthy();
  });

  it('shows no kept tests section when the draft reaches none', async () => {
    renderCard();
    await waitFor(() => expect(startDraftKeptTests).toHaveBeenCalledOnce());
    await waitFor(() => expect(screen.queryByRole('heading', { name: 'Kept tests' })).toBeNull());
  });
});

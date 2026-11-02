// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Validation } from '../../../api/types';
import { TooltipProvider } from '../../../ui/tooltip';
import { PublishCard } from './publish-card';

// The card reads the draft session and the editor facts; the test sets them and watches the card.
const state = vi.hoisted(() => ({ hasDraft: true }));
const closeDraft = vi.hoisted(() => vi.fn(() => {
  state.hasDraft = false;
}));
const publishDraft = vi.hoisted(() => vi.fn(async () => ({ number: 5 })));

vi.mock('../../../api/definitions', () => ({ publishDraft }));
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

beforeEach(() => {
  state.hasDraft = true;
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
});

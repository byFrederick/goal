// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, answerItem } from '../../api/client';
import type { Factory, Item, ItemQuestion, OrgSummary } from '../../api/types';
import { reasons } from '../../shared/permissions';
import { renderFactoryPage } from '../../test/route';
import { TooltipProvider } from '../../ui/tooltip';
import { QuestionCard } from './question-card';

vi.mock(import('../../api/client'), async (original) => ({
  ...(await original()),
  answerItem: vi.fn(async () => ({}) as Item),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'goal',
  title: 'Goal',
  work_types: ['feature'],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'approver',
  can: ['item:operate', 'item:decide'],
};

const key = 'goal#147';

/** The item's open question, asked twelve minutes ago with two quick answers. */
function question(overrides: Partial<ItemQuestion> = {}): ItemQuestion {
  return {
    text: 'Should the sidebar show the whole organization?',
    choices: ['The whole organization', 'Only this factory'],
    asked_at: new Date(Date.now() - 12 * 60_000).toISOString(),
    agent: 'frontend-engineer',
    run_id: 'run-1',
    ...overrides,
  };
}

function renderCard(q: ItemQuestion = question(), grants: Factory = factory) {
  return renderFactoryPage({
    page: () => (
      <TooltipProvider>
        <QuestionCard itemKey={key} factory={grants} question={q} />
      </TooltipProvider>
    ),
    path: 'items/$ref',
    location: 'items/147',
    org,
    factory: grants,
  });
}

describe('QuestionCard', () => {
  it('shows who asks, how long ago, that the run is paused, and the question', async () => {
    renderCard();

    expect(await screen.findByRole('heading', { name: 'frontend-engineer asks you' })).toBeTruthy();
    expect(screen.getByText('12 min ago, the run is paused')).toBeTruthy();
    expect(screen.getByText('Should the sidebar show the whole organization?')).toBeTruthy();
  });

  it('offers one quick answer per choice and a Write an answer button', async () => {
    renderCard();

    expect(await screen.findByRole('button', { name: 'The whole organization' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Only this factory' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Write an answer' })).toBeTruthy();
  });

  it('answers with the index of the chosen quick answer', async () => {
    renderCard();

    fireEvent.click(await screen.findByRole('button', { name: 'Only this factory' }));

    await waitFor(() => expect(answerItem).toHaveBeenCalledWith('org-1', key, { choice: 1 }));
  });

  it('opens a text field from Write an answer and sends the trimmed words', async () => {
    renderCard();

    fireEvent.click(await screen.findByRole('button', { name: 'Write an answer' }));
    const field = screen.getByRole('textbox', { name: 'Your answer' });
    fireEvent.change(field, { target: { value: '  Use the weekly budget.  ' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send' }));

    await waitFor(() => expect(answerItem).toHaveBeenCalledWith('org-1', key, { answer: 'Use the weekly budget.' }));
  });

  it('keeps Send disabled until the text has words in it', async () => {
    renderCard();

    fireEvent.click(await screen.findByRole('button', { name: 'Write an answer' }));
    fireEvent.change(screen.getByRole('textbox', { name: 'Your answer' }), { target: { value: '   ' } });

    expect((screen.getByRole('button', { name: 'Send' }) as HTMLButtonElement).disabled).toBe(true);
    expect(answerItem).not.toHaveBeenCalled();
  });

  it('disables every answer while one is being sent', async () => {
    vi.mocked(answerItem).mockReturnValueOnce(new Promise(() => {}));
    renderCard();

    fireEvent.click(await screen.findByRole('button', { name: 'The whole organization' }));

    await waitFor(() =>
      expect((screen.getByRole('button', { name: 'Only this factory' }) as HTMLButtonElement).disabled).toBe(true),
    );
  });

  it('shows a refusal from goald inline, as its problem detail', async () => {
    vi.mocked(answerItem).mockRejectedValueOnce(
      new ApiError({ title: 'Conflict', status: 409, code: 'no-open-question', detail: 'goal#147 has no open question' }),
    );
    renderCard();

    fireEvent.click(await screen.findByRole('button', { name: 'Only this factory' }));

    expect((await screen.findByRole('alert')).textContent).toBe('goal#147 has no open question');
  });

  it('shows a bad choice from goald inline', async () => {
    vi.mocked(answerItem).mockRejectedValueOnce(
      new ApiError({ title: 'Bad Request', status: 400, code: 'bad-choice', detail: 'choice 5 is not offered' }),
    );
    renderCard();

    fireEvent.click(await screen.findByRole('button', { name: 'Only this factory' }));

    expect((await screen.findByRole('alert')).textContent).toBe('choice 5 is not offered');
  });

  it('keeps the answers in view but disabled, saying who decides, without item:decide', async () => {
    renderCard(question(), { ...factory, can: ['item:operate'] });

    const choice = await screen.findByRole('button', { name: 'Only this factory' });
    expect(choice.getAttribute('aria-disabled')).toBe('true');
    expect(choice.getAttribute('aria-describedby')).toBeTruthy();
    const describedBy = document.getElementById(choice.getAttribute('aria-describedby') ?? '');
    expect(describedBy?.textContent).toBe(reasons.decide('goal'));

    const write = screen.getByRole('button', { name: 'Write an answer' });
    expect(write.getAttribute('aria-disabled')).toBe('true');
    const send = screen.getByRole('button', { name: 'Send' });
    expect(send.getAttribute('aria-disabled')).toBe('true');

    fireEvent.click(choice);
    expect(answerItem).not.toHaveBeenCalled();
    expect(within(document.body).getByText(reasons.decide('goal'))).toBeTruthy();
  });
});

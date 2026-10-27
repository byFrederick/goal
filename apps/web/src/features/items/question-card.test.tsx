// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import type { ReactElement } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { InboxEntry } from '../../api/types';
import { TooltipProvider } from '../../ui/tooltip';
import { QuestionCard } from './question-card';

afterEach(cleanup);

const renderCard = (ui: ReactElement) => render(<TooltipProvider>{ui}</TooltipProvider>);

/** An open decision on the item, asked twelve minutes ago, whose brief says the question. */
function decision(summary: string | null): InboxEntry {
  return {
    id: 'decision:item-147',
    kind: 'decision',
    factory: { id: 'factory-1', name: 'software', title: null },
    since: new Date(Date.now() - 12 * 60_000).toISOString(),
    item: null,
    station: 'build',
    action: null,
    approval: summary === null ? null : { summary, facts: [], pull_request: null, spend_usd: 2.38 },
    blocked: null,
    andon: null,
    rework: null,
  } as unknown as InboxEntry;
}

describe('QuestionCard', () => {
  it('shows who asks, how long ago, whether the run is paused, and the question', () => {
    renderCard(
      <QuestionCard
        entry={decision('Should the sidebar show the whole organization?')}
        agent="Frontend-engineer"
        paused
        onWriteAnswer={() => {}}
      />,
    );

    expect(screen.getByRole('heading', { name: 'Frontend-engineer asks you' })).toBeTruthy();
    expect(screen.getByText('12 min ago, the run is paused')).toBeTruthy();
    expect(screen.getByText('Should the sidebar show the whole organization?')).toBeTruthy();
  });

  it('keeps the quick answers inert, with the "Not available yet" tip', () => {
    renderCard(
      <QuestionCard entry={decision('A question?')} agent="Architect" paused={false} onWriteAnswer={() => {}} />,
    );

    for (const answer of ['Yes', 'No']) {
      const button = screen.getByRole('button', { name: answer });
      expect(button.getAttribute('aria-disabled')).toBe('true');
      fireEvent.click(button);
    }
  });

  it('moves the user to the note field from "Write an answer"', () => {
    const onWriteAnswer = vi.fn();
    renderCard(
      <QuestionCard entry={decision('A question?')} agent="Architect" paused={false} onWriteAnswer={onWriteAnswer} />,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Write an answer' }));
    expect(onWriteAnswer).toHaveBeenCalledTimes(1);
  });

  it('renders nothing when the decision has no question', () => {
    const { container } = renderCard(
      <QuestionCard entry={decision(null)} agent="Architect" paused={false} onWriteAnswer={() => {}} />,
    );
    expect(container.textContent).toBe('');
  });
});

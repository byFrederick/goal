// The agent's question to a person: the you-tint panel above the item's tabs. It renders from the
// item's open Needs you entry (a decision): the API has no question event yet, so the entry's brief
// summary is the question. Its quick answers are not built yet; they wait behind NotAvailable.
import { useId } from 'react';
import type { InboxEntry } from '../../api/types';
import { formatAge } from '../../shared/format';
import { Button } from '../../ui/button';
import { cn } from '../../ui/cn';
import { NotAvailable } from '../../ui/not-available';

/** The quick answers the agent offers once "Agents ask questions" lands. Placeholders until then. */
const quickAnswers = ['Yes', 'No'];

interface Props {
  entry: InboxEntry;
  /** The agent that asks, by its role: never a copy number. */
  agent: string | null;
  /** Whether the item's run is paused: the card says so. */
  paused: boolean;
  onWriteAnswer: () => void;
  className?: string;
}

export function QuestionCard({ entry, agent, paused, onWriteAnswer, className }: Props) {
  const id = useId();
  const question = entry.approval?.summary?.trim();
  if (!question) return null;

  const ago = formatAge(Math.max(0, (Date.now() - Date.parse(entry.since)) / 1000));
  const when = ago === 'now' ? 'Just now' : `${ago} ago`;

  return (
    <section aria-labelledby={id} className={cn('flex min-w-0 flex-col gap-3 rounded-card bg-you-tint p-4', className)}>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <h2 id={id} className="m-0 text-body font-medium text-you-ink">
          {agent ? `${agent} asks you` : 'An agent asks you'}
        </h2>
        <span className="ml-auto text-secondary text-muted">
          {when}
          {paused ? ', the run is paused' : ''}
        </span>
      </div>
      <p className="m-0 text-heading leading-normal font-medium text-ink">{question}</p>
      {/* Phones: the quick answers and Write an answer are full-width 44 px targets, one under another. */}
      <div className="flex flex-wrap gap-2 max-md:flex-col">
        {quickAnswers.map((answer) => (
          <NotAvailable key={answer} label="Quick answer">
            <Button variant="secondary" type="button" className="border-you-tint max-md:h-11">
              {answer}
            </Button>
          </NotAvailable>
        ))}
        <Button variant="ghost" type="button" className="text-you-ink max-md:h-11" onClick={onWriteAnswer}>
          Write an answer
        </Button>
      </div>
    </section>
  );
}

// The agent's question to a person: the you-tint panel above the item's tabs (ADR 0018). It renders the
// item's open question (Item.question): the agent's choices as quick answers, and Write an answer for
// words of the person's own. Both answer through answerItem. No question, no card.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { type FormEvent, useEffect, useId, useRef, useState } from 'react';
import { ApiError, answerItem } from '../../api/client';
import { keys } from '../../api/queries';
import type { Factory, ItemAnswer, ItemQuestion } from '../../api/types';
import { formatAge } from '../../shared/format';
import { itemChangeInvalidations } from '../../shared/item-actions';
import { useOrg } from '../../shared/org';
import { holds, reasons } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { cn } from '../../ui/cn';

/** The longest answer in the person's own words (goald's ItemAnswer). */
const maxAnswer = 2000;

interface Props {
  /** The item's id or ref, as answerItem takes it. */
  itemKey: string;
  /** The factory the item is in. Its `can` decides whether the answers are offered; without it, they wait. */
  factory: Pick<Factory, 'id' | 'name'> & Partial<Pick<Factory, 'can' | 'archived_at'>>;
  question: ItemQuestion;
  className?: string;
}

export function QuestionCard({ itemKey, factory, question, className }: Props) {
  const id = useId();
  const reasonId = useId();
  const writerId = useId();
  const org = useOrg();
  const client = useQueryClient();
  const mayDecide = holds(factory, 'item:decide');
  const [writing, setWriting] = useState(false);
  const [text, setText] = useState('');
  const field = useRef<HTMLTextAreaElement>(null);

  const answer = useMutation({
    mutationFn: (body: ItemAnswer) => answerItem(org.id, itemKey, body),
    // goald answers with the item after the answer: the card goes at once, the queries then follow.
    onSuccess: (updated) => {
      client.setQueryData(keys.item(org.id, itemKey), updated);
    },
    onSettled: () =>
      Promise.all(
        [...itemChangeInvalidations(org.id, factory.id, itemKey), keys.events(org.id, itemKey)].map((queryKey) =>
          client.invalidateQueries({ queryKey }),
        ),
      ),
  });

  useEffect(() => {
    if (writing) field.current?.focus();
  }, [writing]);

  const ago = formatAge(Math.max(0, (Date.now() - Date.parse(question.asked_at)) / 1000));
  const when = ago === 'now' ? 'Just now' : `${ago} ago`;
  const trimmed = text.trim();
  const sendable = trimmed !== '' && [...trimmed].length <= maxAnswer;
  // An answer the caller may not give stays in view, disabled and focusable, saying who decides.
  const decision = (act: () => void) =>
    mayDecide
      ? { disabled: answer.isPending, onClick: act }
      : { 'aria-disabled': true, 'aria-describedby': reasonId, onClick: undefined };
  const showWriter = writing || !mayDecide;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!mayDecide || !sendable) return;
    answer.mutate({ answer: trimmed });
  };

  return (
    <section aria-labelledby={id} className={cn('flex min-w-0 flex-col gap-3 rounded-card bg-you-tint p-4', className)}>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <h2 id={id} className="m-0 text-body font-medium text-you-ink">
          {`${question.agent} asks you`}
        </h2>
        <span className="ml-auto text-secondary text-muted">{`${when}, the run is paused`}</span>
      </div>
      <p className="m-0 text-heading leading-normal font-medium text-ink">{question.text}</p>
      {/* Phones: the quick answers and Write an answer are full-width 44 px targets, one under another. */}
      <div className="flex flex-wrap gap-2 max-md:flex-col">
        {question.choices.map((choice, index) => (
          <Button
            key={choice}
            variant="secondary"
            type="button"
            className="border-you/40 max-md:h-11"
            {...decision(() => answer.mutate({ choice: index }))}
          >
            {choice}
          </Button>
        ))}
        <Button
          variant="ghost"
          type="button"
          className="text-you-ink max-md:h-11"
          {...decision(() => setWriting(true))}
        >
          Write an answer
        </Button>
      </div>
      {showWriter ? (
        <form onSubmit={submit} className="flex flex-col gap-2">
          <label htmlFor={writerId} className="sr-only">
            Your answer
          </label>
          <textarea
            id={writerId}
            ref={field}
            value={text}
            maxLength={maxAnswer}
            rows={2}
            disabled={!mayDecide || answer.isPending}
            onChange={(e) => setText(e.target.value)}
            className="min-h-20 w-full resize-y rounded-control border border-rule bg-card px-3 py-2 text-body text-ink"
          />
          <div className="flex items-center gap-2">
            <Button
              type="submit"
              variant="primary"
              className="ml-auto h-11 px-4 text-body max-md:w-full"
              {...(mayDecide
                ? { disabled: !sendable || answer.isPending }
                : { 'aria-disabled': true, 'aria-describedby': reasonId })}
            >
              Send
            </Button>
          </div>
        </form>
      ) : null}
      {mayDecide ? null : (
        <p id={reasonId} className="m-0 text-secondary text-muted">
          {reasons.decide(factory.name)}
        </p>
      )}
      {answer.isError ? (
        <p role="alert" className="m-0 text-secondary text-rework-ink">
          {answer.error instanceof ApiError
            ? (answer.error.problem.detail ?? answer.error.message)
            : answer.error.message}
        </p>
      ) : null}
    </section>
  );
}

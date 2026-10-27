// "Note for the agent": a person's note for the agents working an item (E14a). goald records it on
// the item and the item's next run reads it in its brief; the composer says so.
// Notes need item:operate (approvers and editors); without it the composer says who sends them.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { type FormEvent, type Ref, useId, useState } from 'react';
import { noteItem } from '../../api/client';
import { keys } from '../../api/queries';
import type { Factory, ItemState, NoteReceipt } from '../../api/types';
import { useOrg } from '../../shared/org';
import { holds, reasons } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { cn } from '../../ui/cn';

/** goald's limit on a note, in characters. */
const maxNote = 2000;

const delivered: Record<NoteReceipt['delivery'], string> = {
  next_run: 'Will be in the next run',
  live: 'Delivered',
};

interface Props {
  /** The id or ref the API accepts. */
  itemKey: string;
  /** The item's factory: its name and what the caller may do there. */
  factory: Pick<Factory, 'name' | 'can'>;
  state: ItemState;
  /** Wide screens send with "Send note"; phones with a 48 px "Send" beside the field. */
  compact?: boolean;
  /** Where "Write an answer" puts the focus: the field. */
  fieldRef?: Ref<HTMLTextAreaElement>;
  className?: string;
}

export function NoteComposer({ itemKey, factory, state, compact = false, fieldRef, className }: Props) {
  const org = useOrg();
  const client = useQueryClient();
  const id = useId();
  const [text, setText] = useState('');
  const note = useMutation({
    mutationFn: (t: string) => noteItem(org.id, itemKey, t),
    onSuccess: () => setText(''),
    // The note is the item's newest event: the trace shows it.
    onSettled: () => client.invalidateQueries({ queryKey: keys.item(org.id, itemKey) }),
  });

  if (state === 'done' || state === 'cancelled') return null;
  if (!holds(factory, 'item:operate')) {
    return <p className={cn('m-0 text-secondary text-muted', className)}>{reasons.note(factory.name)}</p>;
  }

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const t = text.trim();
    if (t) note.mutate(t);
  };
  const statusId = `${id}-status`;

  return (
    <form
      onSubmit={submit}
      className={cn('flex flex-col gap-3 rounded-card border border-line bg-card p-4', className)}
    >
      <label htmlFor={id} className="text-body font-medium">
        Note for the agent
      </label>
      <textarea
        id={id}
        ref={fieldRef}
        value={text}
        maxLength={maxNote}
        rows={compact ? 2 : 3}
        onChange={(e) => setText(e.target.value)}
        placeholder="It reads your note between turns, without stopping."
        aria-describedby={statusId}
        className="min-h-20 w-full resize-y rounded-control border border-rule bg-card px-3 py-2 text-body text-ink transition-colors duration-200 hover:border-muted"
      />
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-secondary text-muted">Counts as a touch on this item</span>
        <Button
          type="submit"
          variant="primary"
          className={cn('ml-auto px-4', compact ? 'h-12 text-body' : 'h-11 text-body')}
          disabled={note.isPending || text.trim() === ''}
        >
          {compact ? 'Send' : 'Send note'}
        </Button>
      </div>
      <p id={statusId} role="status" className="m-0 min-h-5 text-secondary text-muted">
        {note.isSuccess ? delivered[note.data.delivery] : ''}
      </p>
      {note.isError ? (
        <p role="alert" className="m-0 text-secondary text-rework-ink">
          Could not send the note: {note.error.message}
        </p>
      ) : null}
    </form>
  );
}

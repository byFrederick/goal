// "Note to the investigator": a person's note for the agents working an item (E14a). goald
// records it on the item and the item's next run reads it in its brief; the composer says so.
// Notes need item:operate (approvers and editors); without it the composer says who sends them.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { type FormEvent, useId, useState } from 'react';
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
  /** The agent the note reaches; unknown before any agent worked at the item's station. */
  agent: string | undefined;
  /** Wide screens send with "Send note"; phones with a 48 px "Send" beside the field. */
  compact?: boolean;
  className?: string;
}

export function NoteComposer({ itemKey, factory, state, agent, compact = false, className }: Props) {
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
    return <p className={cn('m-0 text-[13px] text-muted', className)}>{reasons.note(factory.name)}</p>;
  }

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const t = text.trim();
    if (t) note.mutate(t);
  };
  const statusId = `${id}-status`;

  return (
    <form onSubmit={submit} className={cn('flex flex-col gap-1.5', className)}>
      <label htmlFor={id} className="text-sm text-muted">
        {agent ? `Note to the ${agent}` : 'Note to the next agent'}
      </label>
      <div className="flex flex-wrap items-end gap-2">
        <input
          id={id}
          type="text"
          value={text}
          maxLength={maxNote}
          onChange={(e) => setText(e.target.value)}
          placeholder="What should the agent know?"
          aria-describedby={statusId}
          className={cn(
            'min-w-0 flex-1 basis-[240px] rounded-control border border-rule bg-card px-3 text-base text-ink transition-colors duration-200 hover:border-muted',
            compact ? 'h-12' : 'h-11 md:text-[15px]',
          )}
        />
        <Button
          type="submit"
          variant="primary"
          className={compact ? 'h-12 px-4 text-[15px]' : 'h-11 px-4 text-[15px]'}
          disabled={note.isPending || text.trim() === ''}
        >
          {compact ? 'Send' : 'Send note'}
        </Button>
      </div>
      <p id={statusId} role="status" className="m-0 min-h-5 text-[13px] text-muted">
        {note.isSuccess ? delivered[note.data.delivery] : ''}
      </p>
      {note.isError ? (
        <p role="alert" className="m-0 text-[13px] text-rework-ink">
          Could not send the note: {note.error.message}
        </p>
      ) : null}
    </form>
  );
}

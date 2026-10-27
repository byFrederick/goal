// Add a part (ADR 0012): one more part of an open assembly, from the Assembly page's header. The
// dialog asks for its title, its brief, the lane that should work it (optional) and the existing
// parts it needs. goald queues it at the assembly's own station, so the dialog picks none. A 400
// part-invalid or a 409 refusal (not-an-assembly, item-closed, edge-cycle) is said inside the dialog,
// which stays open; success closes it, says so, and refreshes the assembly and the board.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { type FormEvent, type RefObject, useRef, useState } from 'react';
import { toast } from 'sonner';
import { ApiError, addItemPart } from '../../api/client';
import type { AssemblyPart, BoardLane, NewPartRequest } from '../../api/types';
import { humanize } from '../../shared/format';
import { itemChangeInvalidations } from '../../shared/item-actions';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { Dialog, DialogClose, DialogContent } from '../../ui/dialog';
import { Field, Input, NativeSelect, Textarea } from '../../ui/field';

interface AddPartDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The assembly's factory, by id (the item's query keys). */
  factoryId: string;
  /** The id or ref the API accepts for the assembly. */
  itemKey: string;
  parts: readonly AssemblyPart[];
  /** The board's lanes of the assembly's station; the agent lanes among them can work the part. */
  stationLanes: readonly BoardLane[];
}

const checkbox = 'size-[18px] shrink-0 cursor-pointer accent-ink';
const legend = 'mb-2 p-0 text-sm font-medium';

/**
 * The lanes the Lane select offers: the agents the assembly's parts already use, then the agent lanes
 * of the station. Named by their role; an agent the board does not name reads humanized.
 */
function laneOptions(parts: readonly AssemblyPart[], stationLanes: readonly BoardLane[]) {
  const agents = stationLanes.filter((l) => l.kind === 'agent');
  const keys = new Set([...parts.flatMap((p) => (p.agent ? [p.agent] : [])), ...agents.map((l) => l.key)]);
  return [...keys].map((key) => ({ key, label: agents.find((l) => l.key === key)?.role ?? humanize(key) }));
}

/** Why an add failed, in the dialog's words: goald's detail, or that goald did not answer. */
function failure(err: Error): string {
  if (!(err instanceof ApiError)) return 'goald is not answering. The part is not added; try again once it is.';
  return err.detail ?? err.title;
}

export function AddPartDialog({ open, onOpenChange, factoryId, itemKey, parts, stationLanes }: AddPartDialogProps) {
  const titleRef = useRef<HTMLInputElement>(null);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title="Add a part"
        description="It is queued at the assembly's station and waits for the parts it needs."
        initialFocus={titleRef}
      >
        <AddPartForm
          titleRef={titleRef}
          factoryId={factoryId}
          itemKey={itemKey}
          parts={parts}
          stationLanes={stationLanes}
          onAdded={() => onOpenChange(false)}
        />
      </DialogContent>
    </Dialog>
  );
}

interface AddPartFormProps {
  titleRef: RefObject<HTMLInputElement | null>;
  factoryId: string;
  itemKey: string;
  parts: readonly AssemblyPart[];
  stationLanes: readonly BoardLane[];
  onAdded: () => void;
}

/** The form inside the dialog: it is mounted while the dialog is open, so each opening starts empty. */
function AddPartForm({ titleRef, factoryId, itemKey, parts, stationLanes, onAdded }: AddPartFormProps) {
  const org = useOrg();
  const client = useQueryClient();
  const briefRef = useRef<HTMLTextAreaElement>(null);
  const [title, setTitle] = useState('');
  const [brief, setBrief] = useState('');
  const [lane, setLane] = useState('');
  const [needs, setNeeds] = useState<string[]>([]);
  const [submitted, setSubmitted] = useState(false);

  const add = useMutation({
    mutationFn: (body: NewPartRequest) => addItemPart(org.id, itemKey, body),
    onSuccess: (_assembly, body) => {
      onAdded();
      toast.success(`Added ${body.title}.`);
      return Promise.all(
        itemChangeInvalidations(org.id, factoryId, itemKey).map((queryKey) => client.invalidateQueries({ queryKey })),
      );
    },
  });

  const titleError = submitted && !title.trim() ? 'Enter a title' : undefined;
  const briefError = submitted && !brief.trim() ? 'Enter a brief' : undefined;
  const lanes = laneOptions(parts, stationLanes);

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    setSubmitted(true);
    if (!title.trim() || !brief.trim()) {
      (title.trim() ? briefRef : titleRef).current?.focus();
      return;
    }
    if (add.isPending) return;
    add.mutate({
      title: title.trim(),
      brief: brief.trim(),
      ...(lane ? { lane } : {}),
      ...(needs.length > 0 ? { needs: parts.filter((p) => needs.includes(p.key)).map((p) => p.key) } : {}),
    });
  };

  return (
    <form onSubmit={onSubmit} noValidate className="flex flex-col gap-5">
      {add.error ? (
        <p
          role="alert"
          className="m-0 rounded-control border border-rework-tint bg-rework-tint px-3 py-2.5 text-sm leading-snug text-rework-ink"
        >
          {failure(add.error)}
        </p>
      ) : null}

      <Field label="Title" error={titleError}>
        <Input
          ref={titleRef}
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          maxLength={200}
          autoComplete="off"
        />
      </Field>

      {lanes.length > 0 ? (
        <Field label="Lane">
          <NativeSelect value={lane} onChange={(e) => setLane(e.target.value)}>
            <option value="">Any agent</option>
            {lanes.map((l) => (
              <option key={l.key} value={l.key}>
                {l.label}
              </option>
            ))}
          </NativeSelect>
        </Field>
      ) : null}

      <Field label="Brief" error={briefError}>
        <Textarea ref={briefRef} rows={5} value={brief} onChange={(e) => setBrief(e.target.value)} maxLength={10000} />
      </Field>

      {parts.length > 0 ? (
        <fieldset className="m-0 min-w-0 border-0 p-0">
          <legend className={legend}>What it needs</legend>
          <div className="flex flex-col gap-1">
            {parts.map((part) => (
              <label key={part.id} className="flex min-h-11 cursor-pointer items-center gap-3 text-[15px] md:min-h-8">
                <input
                  type="checkbox"
                  checked={needs.includes(part.key)}
                  onChange={(e) =>
                    setNeeds((n) => (e.target.checked ? [...n, part.key] : n.filter((k) => k !== part.key)))
                  }
                  className={checkbox}
                />
                <span className="min-w-0 flex-1 truncate">{part.title}</span>
                <span className="shrink-0 text-[13px] text-muted">{part.ref}</span>
              </label>
            ))}
          </div>
        </fieldset>
      ) : null}

      <div className="flex flex-wrap justify-end gap-2">
        <DialogClose asChild>
          <Button type="button" variant="secondary">
            Cancel
          </Button>
        </DialogClose>
        <Button type="submit" variant="primary" disabled={add.isPending}>
          {add.isPending ? 'Adding…' : 'Add part'}
        </Button>
      </div>
    </form>
  );
}

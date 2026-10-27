// The Assembly page's header actions (ADR 0012): Add a part opens the dialog that adds one part to
// the assembly (AddPartDialog). Pause and Resume act on the assembly's open parts (pauseItem; retryItem
// sends the blocked parts back), and Cancel assembly cancels them all. Pause asks nothing, as the item
// page's pause only stops a run; Cancel assembly asks first, since it closes every open part. What the
// caller may do comes from the factory's `can`: Add a part needs item:create, Pause needs item:operate,
// Resume and Cancel assembly need item:decide (goald refuses the rest).
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Ban, Pause, Play, Plus } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';
import { ApiError, cancelItem } from '../../api/client';
import type { AssemblyPart, BoardLane, Factory, Item } from '../../api/types';
import { itemChangeInvalidations, useDecision } from '../../shared/item-actions';
import { useOrg } from '../../shared/org';
import { holds } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { Dialog, DialogClose, DialogContent } from '../../ui/dialog';
import { AddPartDialog } from './add-part-dialog';
import { openPartCount, pauseAction } from './assembly-plan';

interface Props {
  item: Item;
  /** The id or ref the API accepts for the assembly. */
  itemKey: string;
  parts: readonly AssemblyPart[];
  factory: Factory;
  /** The board's lanes of the assembly's station, for the Add a part dialog. */
  stationLanes: readonly BoardLane[];
}

/** Add a part, Pause or Resume, and Cancel assembly, in the page's header; nothing when the caller may do none. */
export function AssemblyActions({ item, itemKey, parts, factory, stationLanes }: Props) {
  const org = useOrg();
  const client = useQueryClient();
  const decide = useDecision(
    { key: itemKey, ref: item.ref, title: item.title, state: item.state, pull_requests: [] },
    factory.id,
  );
  const [addOpen, setAddOpen] = useState(false);
  const [cancelOpen, setCancelOpen] = useState(false);
  const cancel = useMutation({
    mutationFn: () => cancelItem(org.id, itemKey),
    onSuccess: () => {
      setCancelOpen(false);
      toast.success(`Cancelled ${item.ref}.`);
    },
    onError: (err) => {
      // A 409 (the assembly closed meanwhile) is said in the dialog; anything else is a toast.
      if (err instanceof ApiError && err.status === 409) return;
      toast.error(`Could not cancel ${item.ref}`, { description: err.message });
    },
    onSettled: () =>
      Promise.all(
        itemChangeInvalidations(org.id, factory.id, itemKey).map((queryKey) => client.invalidateQueries({ queryKey })),
      ),
  });

  const open = item.state !== 'done' && item.state !== 'cancelled';
  const action = open ? pauseAction(parts) : null;
  const showAdd = open && holds(factory, 'item:create');
  const showPause = action === 'pause' && holds(factory, 'item:operate');
  const showResume = action === 'resume' && holds(factory, 'item:decide');
  const showCancel = open && holds(factory, 'item:decide');
  if (!showAdd && !showPause && !showResume && !showCancel) return null;

  const pending = decide.isPending ? decide.variables?.kind : undefined;
  const refusal =
    cancel.error instanceof ApiError && cancel.error.status === 409
      ? (cancel.error.detail ?? cancel.error.title)
      : undefined;

  return (
    <div className="flex flex-wrap items-center gap-2">
      {showAdd ? (
        <>
          <Button variant="white" onClick={() => setAddOpen(true)}>
            <Plus size={16} strokeWidth={1.75} aria-hidden="true" />
            Add a part
          </Button>
          <AddPartDialog
            open={addOpen}
            onOpenChange={setAddOpen}
            factoryId={factory.id}
            itemKey={itemKey}
            parts={parts}
            stationLanes={stationLanes}
          />
        </>
      ) : null}
      {showPause ? (
        <Button
          variant="white"
          disabled={decide.isPending}
          onClick={() => decide.mutate({ kind: 'pause', reason: '' })}
        >
          <Pause size={16} strokeWidth={1.75} aria-hidden="true" />
          {pending === 'pause' ? 'Pausing…' : 'Pause'}
        </Button>
      ) : null}
      {showResume ? (
        <Button variant="primary" disabled={decide.isPending} onClick={() => decide.mutate({ kind: 'retry' })}>
          <Play size={16} strokeWidth={1.75} aria-hidden="true" />
          {pending === 'retry' ? 'Resuming…' : 'Resume'}
        </Button>
      ) : null}
      {showCancel ? (
        <>
          <Button variant="rework" disabled={cancel.isPending} onClick={() => setCancelOpen(true)}>
            <Ban size={16} strokeWidth={1.75} aria-hidden="true" />
            {cancel.isPending ? 'Cancelling…' : 'Cancel assembly'}
          </Button>
          <CancelDialog
            openParts={openPartCount(parts)}
            working={parts.some((p) => p.state === 'in_progress')}
            open={cancelOpen}
            onOpenChange={(o) => {
              setCancelOpen(o);
              // A refusal said in the dialog goes with it.
              if (!o) cancel.reset();
            }}
            onConfirm={() => cancel.mutate()}
            pending={cancel.isPending}
            refusal={refusal}
          />
        </>
      ) : null}
    </div>
  );
}

interface CancelDialogProps {
  openParts: number;
  /** Whether a part is being worked now: its agent stops. */
  working: boolean;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
  pending: boolean;
  refusal: string | undefined;
}

/** Names how many open parts the cancel closes; Keep it leaves them as they are. */
function CancelDialog({ openParts, working, open, onOpenChange, onConfirm, pending, refusal }: CancelDialogProps) {
  const cascade =
    openParts === 1
      ? 'Its 1 open part is cancelled too.'
      : openParts > 1
        ? `Its ${openParts} open parts are cancelled too.`
        : '';
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={['Cancel this assembly?', cascade].filter(Boolean).join(' ')}
        description={working ? 'The agents working on its parts now stop.' : undefined}
      >
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            onConfirm();
          }}
        >
          {refusal ? (
            <p
              role="alert"
              className="m-0 rounded-control border border-rework-tint bg-rework-tint px-3 py-2.5 text-sm leading-snug text-rework-ink"
            >
              {refusal}
            </p>
          ) : null}
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary">Keep it</Button>
            </DialogClose>
            <Button type="submit" variant="rework" disabled={pending}>
              <Ban size={16} strokeWidth={1.75} aria-hidden="true" />
              {pending ? 'Cancelling…' : 'Cancel assembly'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

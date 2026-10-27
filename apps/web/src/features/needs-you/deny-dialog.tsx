// Deny asks for the reason first: the agent reads it before it reworks the item. Submitting an empty
// reason is refused here, as the item page's send back is.
import { type FormEvent, useId, useState } from 'react';
import { Button } from '../../ui/button';
import { Dialog, DialogClose, DialogContent } from '../../ui/dialog';

export function DenyDialog({
  ref: itemRef,
  open,
  onOpenChange,
  onConfirm,
  pending,
}: {
  /** The item's ref, for the title. */
  ref: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: (reason: string) => void;
  pending: boolean;
}) {
  const id = useId();
  const [reason, setReason] = useState('');
  const [touched, setTouched] = useState(false);
  const missing = reason.trim() === '';

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (!missing) onConfirm(reason.trim());
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        onOpenChange(o);
        if (!o) {
          setReason('');
          setTouched(false);
        }
      }}
    >
      <DialogContent
        title={`Deny ${itemRef}`}
        description="It goes back to the agent for rework. Say what must change; the agent reads your reason first."
      >
        <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
          <div className="flex flex-col gap-2">
            <label htmlFor={id} className="text-secondary text-muted">
              What must change
            </label>
            <textarea
              id={id}
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              rows={4}
              required
              aria-invalid={touched && missing}
              aria-describedby={touched && missing ? `${id}-error` : undefined}
              className="rounded-control border border-rule bg-card p-3 text-body text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-run"
            />
            {touched && missing ? (
              <p id={`${id}-error`} className="m-0 text-secondary text-rework-ink">
                Say what must change.
              </p>
            ) : null}
          </div>
          <div className="flex justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary">Cancel</Button>
            </DialogClose>
            <Button type="submit" variant="danger-fill" disabled={pending}>
              {pending ? 'Denying…' : 'Deny'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

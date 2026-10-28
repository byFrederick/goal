// A modal that asks before an action (C §10.11 `ConfirmDialog`). With `confirmText` the person
// types it (an organization's name) before the action enables, and the field takes focus;
// otherwise the cancel button does, so Enter never confirms by accident.
import { type FormEvent, type ReactNode, useRef, useState } from 'react';
import { Button } from './button';
import { Dialog, DialogClose, DialogContent } from './dialog';
import { Field, Input } from './field';

interface ConfirmDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  description?: ReactNode;
  /** The action, as the button says it: "Delete organization", "Leave". */
  confirmLabel: string;
  /** Shown while the action runs. */
  pendingLabel?: string;
  cancelLabel?: string;
  /** danger: an action that destroys something; its final button is the red-fill rework variant (MASTER.md allows it for a final confirmation). */
  tone?: 'default' | 'danger';
  /** What the person must type, exactly, before the action enables. */
  confirmText?: string;
  pending?: boolean;
  onConfirm: () => void;
  /** Between the description and the buttons: what the action affects, an error. */
  children?: ReactNode;
}

export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  pendingLabel,
  cancelLabel = 'Cancel',
  tone = 'default',
  confirmText,
  pending = false,
  onConfirm,
  children,
}: ConfirmDialogProps) {
  const [typed, setTyped] = useState('');
  const typedRef = useRef<HTMLInputElement>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const confirmed = confirmText === undefined || typed === confirmText;

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    // React bubbles submit through portals: the form the dialog was opened from must not see it.
    e.stopPropagation();
    if (confirmed && !pending) onConfirm();
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) setTyped('');
        onOpenChange(next);
      }}
    >
      <DialogContent title={title} description={description} initialFocus={confirmText ? typedRef : cancelRef}>
        <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
          {children}
          {confirmText !== undefined ? (
            <Field
              label={
                <>
                  Type <strong className="font-semibold">{confirmText}</strong> to confirm
                </>
              }
            >
              <Input
                ref={typedRef}
                value={typed}
                onChange={(e) => setTyped(e.target.value)}
                autoComplete="off"
                autoCapitalize="off"
                spellCheck={false}
              />
            </Field>
          ) : null}
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button ref={cancelRef} variant="secondary" size="lg" className="md:h-10 md:text-sm">
                {cancelLabel}
              </Button>
            </DialogClose>
            <Button
              type="submit"
              variant={tone === 'danger' ? 'rework' : 'primary'}
              size="lg"
              className="md:h-10 md:text-sm"
              disabled={!confirmed || pending}
            >
              {pending ? (pendingLabel ?? confirmLabel) : confirmLabel}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

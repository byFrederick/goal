// Replace secret: one dialog that takes a new value and hands it to goald once, then clears it;
// goald seals it and never answers it. A fresh sign-in is asked first when the session is older
// than 10 minutes (session.ts). The Secrets pages replace a secret's value with it.
import { useMutation } from '@tanstack/react-query';
import { type FormEvent, useRef, useState } from 'react';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Dialog, DialogClose, DialogContent } from '../../ui/dialog';
import { Field } from '../../ui/field';
import { WriteOnly } from './parts';
import { SecretInput } from './secret-input';
import { refusalText } from './words';

/**
 * A dialog that takes one new secret value and hands it to `save` once. The value is cleared when
 * the save succeeds or the dialog closes; `onSaved` then tells the screen what changed (a toast,
 * the cache). The labels are the caller's: "Replace token of github", "Replace value".
 */
export function SecretReplaceDialog<Saved>({
  open,
  title,
  label,
  help,
  actionLabel,
  pendingLabel,
  save,
  onSaved,
  onClose,
}: {
  open: boolean;
  title: string;
  /** The field's name on the form ("API token"). */
  label: string;
  help?: string | undefined;
  /** The submit button's text when idle, and while the save is pending. */
  actionLabel: string;
  pendingLabel: string;
  save: (value: string) => Promise<Saved>;
  onSaved: (saved: Saved) => Promise<void> | void;
  onClose: () => void;
}) {
  const [value, setValue] = useState('');
  const inputRef = useRef<HTMLInputElement>(null);

  const replace = useMutation({
    // The secret is in mutationFn's closure: forget the mutation once nothing shows it.
    gcTime: 0,
    mutationFn: () => save(value),
    onSuccess: async (saved) => {
      setValue('');
      onClose();
      await onSaved(saved);
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (open && value !== '' && !replace.isPending) replace.mutate();
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next) return;
        setValue('');
        replace.reset();
        onClose();
      }}
    >
      <DialogContent
        title={title}
        description="Never shown again, not even to you. It applies from the next run."
        initialFocus={inputRef}
      >
        <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
          <Field label={label} aside={<WriteOnly />} help={help}>
            <SecretInput ref={inputRef} value={value} onChange={(e) => setValue(e.target.value)} />
          </Field>
          {replace.error ? (
            <div role="alert">
              <Callout tone="rework">{refusalText(replace.error)}</Callout>
            </div>
          ) : null}
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary">Cancel</Button>
            </DialogClose>
            <Button type="submit" variant="primary" disabled={value === '' || replace.isPending}>
              {replace.isPending ? pendingLabel : actionLabel}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

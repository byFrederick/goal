// Save the Items page's filters as a saved view, or rename one (ADR 0020). A new view is personal, or
// shared with the factory when the person holds factory:manage. A shared view's sharing is fixed when it
// is made, so a rename changes only the name. A refusal (view-name-taken, invalid-view) stays in the
// dialog, where the person can read it and change the name.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { type FormEvent, useId, useRef, useState } from 'react';
import { ApiError, createSavedView, updateSavedView } from '../../api/client';
import { keys } from '../../api/queries';
import type { Factory, OrgSummary, SavedView, SavedViewFilters } from '../../api/types';
import { holds } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Dialog, DialogContent } from '../../ui/dialog';
import { Field, Input } from '../../ui/field';
import { Switch } from '../../ui/switch';

interface SaveViewDialogProps {
  org: OrgSummary;
  factory: Factory;
  /** The view a rename changes; without it the dialog saves a new one. */
  view?: SavedView | undefined;
  /** What the view keeps: the page's filters for a new view, the renamed view's own filters for a rename. */
  filters: SavedViewFilters;
  onClose: () => void;
}

/** The API's problem detail for a refusal; the message, or the fallback, for anything else. */
export function failureOf(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.detail ?? error.message;
  return fallback;
}

export function SaveViewDialog({ org, factory, view, filters, onClose }: SaveViewDialogProps) {
  const [name, setName] = useState(view?.name ?? '');
  const [shared, setShared] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const nameRef = useRef<HTMLInputElement>(null);
  const sharedId = useId();
  const queryClient = useQueryClient();
  const canShare = view === undefined && holds(factory, 'factory:manage');

  const save = useMutation({
    mutationFn: () =>
      view
        ? updateSavedView(org.id, factory.id, view.id, { name: name.trim(), filters })
        : createSavedView(org.id, factory.id, { name: name.trim(), shared, filters }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: keys.views(org.id, factory.id) });
      onClose();
    },
  });

  const nameError = name.trim() === '' ? 'Give the view a name.' : undefined;

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    // React bubbles submit through portals: the form the dialog was opened from must not see it.
    e.stopPropagation();
    setSubmitted(true);
    if (nameError) {
      nameRef.current?.focus();
      return;
    }
    if (!save.isPending) save.mutate();
  };

  return (
    <Dialog open onOpenChange={(open) => (open ? undefined : onClose())}>
      <DialogContent title={view ? 'Rename view' : 'Save view'} initialFocus={nameRef}>
        <form onSubmit={onSubmit} noValidate className="flex flex-col gap-4">
          <Field label="Name" error={submitted ? nameError : undefined}>
            <Input
              ref={nameRef}
              value={name}
              onChange={(e) => {
                if (save.error) save.reset();
                setName(e.target.value);
              }}
              maxLength={60}
              autoComplete="off"
            />
          </Field>

          {canShare ? (
            <div className="flex items-center justify-between gap-3">
              <span id={sharedId} className="text-body text-ink">
                Share with the factory
              </span>
              <Switch checked={shared} onCheckedChange={setShared} aria-labelledby={sharedId} />
            </div>
          ) : null}

          {save.isError ? (
            <div aria-live="polite">
              <Callout tone="rework">{failureOf(save.error, 'Could not save the view. Try again.')}</Callout>
            </div>
          ) : null}

          <div className="flex flex-wrap justify-end gap-2">
            <Button type="button" variant="secondary" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" variant="primary" disabled={save.isPending}>
              {view ? 'Rename' : 'Save view'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// Save changes: publishes the open draft as the factory's next version, the same way the Definition
// editor's publish does (flush, refuse unsaved edits, publish at the session's revision, close the draft,
// refetch). The draft is shared: everything in it publishes, not only the settings on this page, so the
// line beside the button names the version it publishes. The button sits in the top bar, outside the draft
// provider, so the page calls useSaveChanges inside it and hands the result to SaveChangesAction. goald's
// refusal is shown as it words it, in the page body.
import { useQueryClient } from '@tanstack/react-query';
import { Check } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { ApiError } from '../../api/client';
import { publishDraft } from '../../api/definitions';
import { defKeys } from '../../api/queries';
import type { Factory } from '../../api/types';
import { Button } from '../../ui/button';
import { useDraft, useDraftSession } from '../factory-editor/draft/use-draft';

export interface SaveChangesState {
  save: () => void;
  pending: boolean;
  /** True for a moment after a publish succeeds: the button shows a check. */
  done: boolean;
  disabled: boolean;
  next: number;
  refusal: string | null;
}

export function useSaveChanges(org: string, factory: Factory): SaveChangesState {
  const session = useDraftSession();
  const client = useQueryClient();
  const hasDraft = useDraft((s) => s.hasDraft);
  const [pending, setPending] = useState(false);
  const [refusal, setRefusal] = useState<string | null>(null);
  const [done, setDone] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  const next = (factory.current?.number ?? 0) + 1;

  const save = async () => {
    if (pending) return;
    setPending(true);
    setRefusal(null);
    try {
      await session.flush();
      if (session.hasUnsaved()) {
        setRefusal('Your last changes aren’t saved yet. Save again once the status line says Autosaved.');
        return;
      }
      const version = await publishDraft(org, factory.id, {
        revision: session.getSnapshot().revision,
        expected_current_version_id: factory.current?.id ?? null,
      });
      session.closeDraft();
      toast(`${factory.name} v${version.number} published`);
      setDone(true);
      clearTimeout(timer.current);
      timer.current = setTimeout(() => setDone(false), 2000);
      await Promise.all(
        [
          defKeys.draft(org, factory.id),
          defKeys.versions(org, factory.id),
          defKeys.versionsOf(org, factory.id),
          defKeys.factory(org, factory.id),
          defKeys.factories(org),
          defKeys.compares(org, factory.id),
        ].map((queryKey) => client.invalidateQueries({ queryKey })),
      );
    } catch (err) {
      setRefusal(
        err instanceof ApiError
          ? `Not published: ${(err.detail ?? err.title).replace(/\.$/, '')}.`
          : 'goald can’t be reached. Your draft stays saved; try again.',
      );
    } finally {
      setPending(false);
    }
  };

  return { save: () => void save(), pending, done, disabled: !hasDraft || pending, next, refusal };
}

/** The top bar's actions: the version Save changes publishes, then the button. */
export function SaveChangesAction({ state }: { state: SaveChangesState }) {
  return (
    <div className="flex items-center gap-3">
      <span className="hidden text-secondary text-muted md:inline">Saving publishes v{state.next}</span>
      <Button variant="primary" onClick={state.save} disabled={state.disabled}>
        {state.pending ? (
          'Saving…'
        ) : state.done ? (
          <>
            <Check size={16} strokeWidth={1.8} aria-hidden="true" />
            Published
          </>
        ) : (
          'Save changes'
        )}
      </Button>
    </div>
  );
}

/** goald's refusal of a publish, in the page body. */
export function SaveRefusal({ refusal }: { refusal: string | null }) {
  return refusal ? (
    <p role="alert" className="m-0 text-secondary text-rework-ink">
      {refusal}
    </p>
  ) : null;
}

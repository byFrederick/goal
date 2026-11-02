// Save changes: publishes the open draft as the factory's next version, the same way the Definition
// editor's publish does (flush, refuse unsaved edits, publish at the session's revision, close the draft,
// refetch). The draft is shared: everything in it publishes, not only the settings on this page, so the
// line beside the button names the version it publishes. The button and the line live in the top bar,
// which sits outside the draft session's provider, so SaveChanges hands the session down to them; the
// refusal stays in the page, as a paragraph at its top. goald's refusal is shown as it words it.
import { useQueryClient } from '@tanstack/react-query';
import { useCallback, useMemo, useState } from 'react';
import { toast } from 'sonner';
import { ApiError } from '../../api/client';
import { publishDraft } from '../../api/definitions';
import { defKeys } from '../../api/queries';
import type { Factory } from '../../api/types';
import { usePhone } from '../../app/phone-chrome';
import { TopBarSlot } from '../../app/top-bar';
import { Button } from '../../ui/button';
import { DraftSessionContext, useDraft, useDraftSession } from '../factory-editor/draft/use-draft';

/** Sets the top bar's Save changes and shows goald's refusal at the top of the page. Inside DraftSessionProvider. */
export function SaveChanges({ org, factory }: { org: string; factory: Factory }) {
  const session = useDraftSession();
  const client = useQueryClient();
  const hasDraft = useDraft((s) => s.hasDraft);
  const [pending, setPending] = useState(false);
  const [refusal, setRefusal] = useState<string | null>(null);
  const next = (factory.current?.number ?? 0) + 1;

  const save = useCallback(async () => {
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
  }, [pending, session, org, factory, client]);

  const phone = usePhone();
  const actions = useMemo(
    () => (
      <DraftSessionContext.Provider value={session}>
        <div className="flex min-w-0 items-center justify-end gap-3">
          {phone ? null : <span className="text-secondary text-muted">Saving publishes v{next}</span>}
          <Button variant="primary" onClick={() => void save()} disabled={!hasDraft || pending}>
            Save changes
          </Button>
        </div>
      </DraftSessionContext.Provider>
    ),
    [session, phone, next, save, hasDraft, pending],
  );

  return (
    <>
      <TopBarSlot actions={actions} />
      {phone ? <p className="m-0 text-secondary text-muted">Saving publishes v{next}</p> : null}
      {refusal ? (
        <p role="alert" className="m-0 max-w-md text-secondary text-rework-ink">
          {refusal}
        </p>
      ) : null}
    </>
  );
}

// Save changes: publishes the open draft as the factory's next version, the same way the Definition
// editor's publish does (flush, refuse unsaved edits, publish at the session's revision, close the draft,
// refetch). The draft is shared: everything in it publishes, not only the settings on this page, so the
// line under the button names the version it publishes. goald's refusal is shown as it words it.
import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { toast } from 'sonner';
import { ApiError } from '../../api/client';
import { publishDraft } from '../../api/definitions';
import { defKeys } from '../../api/queries';
import type { Factory } from '../../api/types';
import { Button } from '../../ui/button';
import { useDraft, useDraftSession } from '../factory-editor/draft/use-draft';

export function SaveChanges({ org, factory }: { org: string; factory: Factory }) {
  const session = useDraftSession();
  const client = useQueryClient();
  const hasDraft = useDraft((s) => s.hasDraft);
  const [pending, setPending] = useState(false);
  const [refusal, setRefusal] = useState<string | null>(null);
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

  return (
    <div className="flex flex-col items-end gap-1">
      <div className="flex flex-wrap items-center gap-3">
        <span className="text-[13px] text-muted">Saving publishes v{next}</span>
        <Button variant="primary" onClick={() => void save()} disabled={!hasDraft || pending}>
          Save changes
        </Button>
      </div>
      {refusal ? (
        <p role="alert" className="m-0 max-w-md text-right text-[13px] leading-snug text-rework-ink">
          {refusal}
        </p>
      ) : null}
    </div>
  );
}

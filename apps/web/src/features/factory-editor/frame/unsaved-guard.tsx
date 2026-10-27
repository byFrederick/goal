// The unsaved-changes guard (D §10.4): moving between the editor's sections only flushes; leaving
// it while anything is unsaved, in flight, or waiting on a retry or a decision first waits up to
// 3 s for the flush, then asks: "Your last changes aren't saved yet" with Retry, Leave anyway and
// Stay. Closing or reloading the tab asks the browser's own question.
import { useBlocker } from '@tanstack/react-router';
import { useState } from 'react';
import { Button } from '../../../ui/button';
import { Dialog, DialogContent } from '../../../ui/dialog';
import type { DraftSession } from '../draft/session';
import { useDraftSession } from '../draft/use-draft';

const flushWaitMs = 3000;

/** Flushes the session, waiting at most `ms`; then whether something is still unsaved. */
async function settles(session: DraftSession, ms: number): Promise<boolean> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  await Promise.race([
    session.flush(),
    new Promise<void>((resolve) => {
      timer = setTimeout(resolve, ms);
    }),
  ]);
  clearTimeout(timer);
  return !session.hasUnsaved();
}

/** Whether `next` is another page of the same editor: its path up to /definition (or the wizard's /new) is the same. */
function sameEditor(current: string, next: string): boolean {
  const root = /^(.*?\/(?:definition|new))(?:\/|$)/.exec(current)?.[1];
  return root !== undefined && (next === root || next.startsWith(`${root}/`));
}

function reason(session: DraftSession): string {
  const s = session.getSnapshot();
  if (s.discarded) return 'The draft was discarded in another tab or the CLI.';
  switch (s.status) {
    case 'offline':
      return 'goald can’t be reached.';
    case 'retrying':
      return 'goald failed to save them.';
    case 'conflict':
      return 'The same fields changed in another tab or the CLI; choose which values to keep first.';
    case 'held':
      return 'factory.yaml doesn’t parse, so Visual changes wait.';
    case 'failed':
      return `goald refused them: ${s.error?.detail ?? s.error?.title ?? 'see the status line'}.`;
    default:
      return 'They are still being saved.';
  }
}

export function UnsavedGuard() {
  const session = useDraftSession();
  const [retrying, setRetrying] = useState(false);
  const [stillUnsaved, setStillUnsaved] = useState(false);

  const blocker = useBlocker({
    shouldBlockFn: async ({ current, next }) => {
      if (sameEditor(current.pathname, next.pathname)) {
        void session.flush();
        return false;
      }
      if (!session.hasUnsaved()) return false;
      return !(await settles(session, flushWaitMs));
    },
    enableBeforeUnload: () => session.hasUnsaved(),
    withResolver: true,
  });

  const retry = async () => {
    setRetrying(true);
    setStillUnsaved(false);
    session.retryNow();
    const saved = await settles(session, flushWaitMs);
    setRetrying(false);
    if (saved) blocker.proceed?.();
    else setStillUnsaved(true);
  };

  const close = () => {
    setStillUnsaved(false);
    blocker.reset?.();
  };

  return (
    <Dialog open={blocker.status === 'blocked'} onOpenChange={(o) => (o ? undefined : close())}>
      <DialogContent title="Your last changes aren’t saved yet" description={reason(session)}>
        {stillUnsaved ? (
          <p role="alert" className="m-0 rounded-control border border-rework-tint bg-rework-tint px-3 py-2.5 text-sm">
            Still not saved. Stay to keep them, or leave without them.
          </p>
        ) : null}
        <div className="flex flex-wrap justify-end gap-2">
          <Button variant="rework" className="h-11 md:h-10" onClick={() => blocker.proceed?.()}>
            Leave anyway
          </Button>
          <Button variant="secondary" className="h-11 md:h-10" onClick={close}>
            Stay
          </Button>
          <Button variant="primary" className="h-11 md:h-10" disabled={retrying} onClick={() => void retry()}>
            {retrying ? 'Retrying…' : 'Retry'}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

// The open draft on Settings: while the factory's draft has changes, the page header shows the Definition
// editor's draft banner with Review changes and Discard draft, so a draft opened from a setting never waits unseen.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { useState } from 'react';
import { draftQuery } from '../../api/queries';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { useDraft, useDraftOpen, useDraftSession, useViewOnly } from '../factory-editor/draft/use-draft';
import { DiscardDialog } from '../factory-editor/frame/discard-dialog';
import { DraftBanner } from '../factory-editor/frame/draft-banner';
import { counts } from '../factory-editor/frame/editor-frame';
import { changesSentence } from '../factory-editor/frame/state';

export function SettingsDraftBanner() {
  const org = useOrg();
  const factory = useFactory();
  const params = useFactoryParams();
  const session = useDraftSession();
  const viewOnly = useViewOnly();
  const s = useDraft((state) => state);
  const open = useDraftOpen(session);
  const draft = useQuery({ ...draftQuery(org.id, factory.id), enabled: open === true && !viewOnly }).data;
  const [discardOpen, setDiscardOpen] = useState(false);
  const { summaries, changeCount, problemCount } = counts(s, draft);
  if (viewOnly || !s.hasDraft || changeCount === 0) return null;
  const current = factory.current?.number ?? draft?.current?.number ?? null;
  const next = draft?.next_number ?? (current !== null ? current + 1 : 1);
  return (
    <>
      <div className="motion-safe:animate-fade-in motion-reduce:animate-fade-in">
        <DraftBanner next={next} current={current} changeCount={changeCount} problemCount={problemCount}>
          <Button asChild variant="white" size="sm">
            <Link to="/o/$org/f/$factory/definition/$section" params={{ ...params, section: 'review' }}>
              Review changes
            </Link>
          </Button>
          {current !== null ? (
            <Button variant="white" size="sm" aria-haspopup="dialog" onClick={() => setDiscardOpen(true)}>
              Discard draft
            </Button>
          ) : null}
        </DraftBanner>
      </div>
      <DiscardDialog
        open={discardOpen}
        onOpenChange={setDiscardOpen}
        next={next}
        current={current}
        changes={changesSentence(changeCount, summaries)}
        changeCount={changeCount}
      />
    </>
  );
}

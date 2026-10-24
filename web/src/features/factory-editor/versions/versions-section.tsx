// Versions (D §10.10): every published version, newest first, a page at a time (B's
// `before`/`limit`), with the rows of Review's history. Compare on a version shows it against the
// draft, or against the version before it when no draft is open; Restore… makes it the running
// version (a pointer move). A lazy section view.
import { useInfiniteQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { listVersions } from '../../../api/definitions';
import { defKeys } from '../../../api/queries';
import type { Version } from '../../../api/types';
import { Button } from '../../../ui/button';
import { SectionCard } from '../../../ui/card';
import { SectionShell } from '../frame/section-shell';
import { ChangesCard } from '../review/changes-card';
import { parseCompare } from '../review/review';
import { useEditorFacts } from '../review/use-review';
import { DraftRow, draftDetail, RestoreDialog, VersionRow } from '../review/version-history';
import type { SectionViewProps } from '../section-views';

const pageSize = 20;

export function VersionsSection({ search }: SectionViewProps) {
  const { org, factory, hasDraft, next, draft } = useEditorFacts();
  const pages = useInfiniteQuery({
    queryKey: [...defKeys.versions(org, factory), 'pages'],
    queryFn: ({ pageParam, signal }) => listVersions(org, factory, { before: pageParam, limit: pageSize }, signal),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (last) => last.next_before ?? undefined,
  });
  const versions = pages.data?.pages.flatMap((p) => p.versions) ?? [];
  const [restoring, setRestoring] = useState<Version | null>(null);
  const pair = parseCompare(search.compare);

  return (
    <div className="grid min-w-0 gap-4 @[960px]:grid-cols-[minmax(0,1fr)_minmax(0,1fr)] @[960px]:items-start">
      <SectionShell section="versions">
        {pages.isPending ? (
          <div aria-busy="true" className="h-40 rounded-control bg-control/60">
            <span className="sr-only">Loading the versions</span>
          </div>
        ) : pages.isError ? (
          <p className="m-0 text-sm text-rework-ink">The versions could not load.</p>
        ) : (
          <>
            <ul className="m-0 list-none p-0 [&>li:first-child]:border-t-0">
              {hasDraft ? <DraftRow label={`Draft of v${next}`} detail={draftDetail(draft?.updated_at)} /> : null}
              {versions.map((v) => (
                <VersionRow key={v.id} version={v} comparing={search.compare} onRestore={setRestoring} />
              ))}
            </ul>
            {versions.length === 0 ? (
              <p className="m-0 text-sm text-muted">Never published. Publish v1 from Review and publish.</p>
            ) : null}
            {pages.hasNextPage ? (
              <div>
                <Button
                  variant="white"
                  className="h-11 md:h-10"
                  disabled={pages.isFetchingNextPage}
                  onClick={() => void pages.fetchNextPage()}
                >
                  {pages.isFetchingNextPage ? 'Loading…' : 'Show older versions'}
                </Button>
              </div>
            ) : null}
          </>
        )}
      </SectionShell>
      {pair ? (
        <ChangesCard pair={pair} />
      ) : (
        <SectionCard title="Compare">
          <p className="m-0 text-sm text-muted">
            Choose Compare on a version to see what it changes
            {hasDraft ? ' against the draft' : ' from the version before it'}.
          </p>
        </SectionCard>
      )}
      <RestoreDialog version={restoring} onClose={() => setRestoring(null)} />
    </div>
  );
}

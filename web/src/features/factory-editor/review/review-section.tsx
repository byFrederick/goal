// Review and publish (D §10.10, boards Define-Publish and New-Review): the alert of the errors that
// block a publish, Checks and the changes (left), Publish, the version history and Budget (right,
// below on narrow screens). Validation runs at the draft's revision, again with the station moves
// chosen for the publish. A lazy section view; it works in the wizard too (D13's last step).
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { validateDraft } from '../../../api/definitions';
import { defKeys, templateQuery, versionsQuery } from '../../../api/queries';
import type { Version } from '../../../api/types';
import { SectionCard } from '../../../ui/card';
import { useModel } from '../draft/use-draft';
import { useValidation } from '../frame/use-editor';
import { SectionLink } from '../search-link';
import type { SectionViewProps } from '../section-views';
import { BudgetCard } from './budget-card';
import { ChangesCard } from './changes-card';
import { ChecksCard, ProblemAlert } from './checks-card';
import { PublishCard } from './publish-card';
import { movesFromProblems, reviewCompare, sourceText } from './review';
import { useEditorFacts } from './use-review';
import { DraftRow, draftDetail, RestoreDialog, VersionRow } from './version-history';

export function ReviewSection({ place, search }: SectionViewProps) {
  const facts = useEditorFacts();
  const { org, factory, hasDraft, revision, draft, current, next, detail } = facts;
  const model = useModel();
  const [moves, setMoves] = useState<Record<string, string>>({});
  const saved = useValidation();
  const moved = useQuery({
    queryKey: [...defKeys.validation(org, factory, revision), 'moves', moves],
    queryFn: ({ signal }) => validateDraft(org, factory, { station_moves: moves }, signal),
    enabled: hasDraft && Object.keys(moves).length > 0,
    placeholderData: keepPreviousData,
  }).data;
  const validation = hasDraft ? (Object.keys(moves).length > 0 ? (moved ?? saved) : saved) : undefined;
  const errors = validation?.problems.filter((p) => p.severity === 'error') ?? [];
  const pair = reviewCompare(search.compare, { place, hasDraft });
  const template = draft?.source.kind === 'template' ? draft.source.template?.split('@') : undefined;
  const templateTitle = useQuery({
    ...templateQuery(org, template?.[0] ?? '', template?.[1] ? Number(template[1]) : undefined),
    enabled: place === 'wizard' && Boolean(template?.[0]),
  }).data?.title;
  const name = detail?.name ?? model.basics.name ?? 'This factory';

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <h2 data-route-focus="" tabIndex={-1} className="sr-only">
        Review and publish
      </h2>
      <ProblemAlert errors={errors} next={next} model={model} />
      <div className="grid min-w-0 gap-4 @[960px]:grid-cols-[minmax(0,1fr)_minmax(320px,410px)] @[960px]:items-start">
        <div className="flex min-w-0 flex-col gap-4">
          {hasDraft ? <ChecksCard validation={validation} model={model} /> : null}
          {pair ? (
            <ChangesCard
              pair={pair}
              fromName={pair.from === 'source' && templateTitle ? `the ${templateTitle} template` : undefined}
              countsOnly={place === 'wizard' && draft?.source.kind === 'blank'}
            />
          ) : (
            <SectionCard title="Changes">
              <p className="m-0 text-sm text-muted">
                {current
                  ? `v${current.number} runs as it is. Compare two versions in the history, or make a change to open a draft.`
                  : 'Nothing published yet.'}
              </p>
            </SectionCard>
          )}
        </div>
        <div className="flex min-w-0 flex-col gap-4">
          <PublishCard
            place={place}
            validation={validation}
            moves={moves}
            onMoves={setMoves}
            needs={movesFromProblems(validation?.problems ?? [])}
          />
          {place === 'wizard' ? (
            <SectionCard title="Versions">
              <ul className="m-0 list-none p-0">
                <DraftRow label={`Draft of v${next}`} detail={draftDetail(draft?.updated_at)} />
                <li className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-3 border-t border-line py-3">
                  <span className="inline-flex h-[22px] items-center rounded-chip border border-line px-[7px] text-xs font-medium">
                    v1
                  </span>
                  <span className="text-[15px] text-muted">Created when you publish</span>
                </li>
                {draft ? (
                  <li className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-3 border-t border-line py-3">
                    <span className="inline-flex h-[22px] items-center rounded-chip border border-line px-[7px] text-xs font-medium">
                      Source
                    </span>
                    <span className="text-[15px] text-muted">{sourceText(draft.source, templateTitle)}</span>
                  </li>
                ) : null}
              </ul>
            </SectionCard>
          ) : (
            <History compare={search.compare} hasDraft={hasDraft} next={next} draftUpdated={draft?.updated_at} />
          )}
          <BudgetCard name={name} />
        </div>
      </div>
    </div>
  );
}

/** The last four versions, "All versions" → the Versions section. */
function History({
  compare,
  hasDraft,
  next,
  draftUpdated,
}: {
  compare: string | undefined;
  hasDraft: boolean;
  next: number;
  draftUpdated: string | undefined;
}) {
  const { org, factory } = useEditorFacts();
  const versions = useQuery(versionsQuery(org, factory)).data?.versions ?? [];
  const [restoring, setRestoring] = useState<Version | null>(null);
  return (
    <SectionCard
      title="Version history"
      aside={
        <SectionLink
          section="versions"
          patch={{}}
          className="pt-1 text-sm underline underline-offset-3 transition-colors duration-200 hover:text-run"
        >
          All versions
        </SectionLink>
      }
    >
      <ul className="m-0 list-none p-0 [&>li:first-child]:border-t-0">
        {hasDraft ? <DraftRow label={`Draft of v${next}`} detail={draftDetail(draftUpdated)} /> : null}
        {versions.slice(0, 4).map((v) => (
          <VersionRow key={v.id} version={v} comparing={compare} onRestore={setRestoring} />
        ))}
      </ul>
      <RestoreDialog version={restoring} onClose={() => setRestoring(null)} />
    </SectionCard>
  );
}

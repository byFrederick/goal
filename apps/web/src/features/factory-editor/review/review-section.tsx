// Review and publish (D §10.10, boards Define-Publish and New-Review): the heading with the change
// count and the checks summary, the alert of the errors that block a publish, the changes and what
// happens to work in progress (left), then Publish, Checks, the version history and Budget (right,
// below on narrow screens). Validation runs at the draft's revision, again with the station moves
// chosen for the publish. A lazy section view; it works in the wizard too (D13's last step).
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { validateDraft } from '../../../api/definitions';
import { defKeys, templateQuery, versionsQuery } from '../../../api/queries';
import type { Version } from '../../../api/types';
import { plural } from '../../../shared/format';
import { SectionCard } from '../../../ui/card';
import { Chip } from '../../../ui/chip';
import { useModel } from '../draft/use-draft';
import { useValidation } from '../frame/use-editor';
import { SectionLink } from '../search-link';
import type { SectionViewProps } from '../section-views';
import { BudgetCard } from './budget-card';
import { ChangesCard } from './changes-card';
import { ChecksCard, ProblemAlert } from './checks-card';
import { PublishCard } from './publish-card';
import { movesFromProblems, reviewCompare, sourceText } from './review';
import { useComparison, useEditorFacts } from './use-review';
import { DraftRow, draftDetail, RestoreDialog, VersionRow } from './version-history';

export function ReviewSection({ place, search }: SectionViewProps) {
  const facts = useEditorFacts();
  const { org, factory, hasDraft, current, next, revision, draft, detail } = facts;
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
  const changes = useComparison(pair).data?.counts.changes;
  const template = draft?.source.kind === 'template' ? draft.source.template?.split('@') : undefined;
  const templateTitle = useQuery({
    ...templateQuery(org, template?.[0] ?? '', template?.[1] ? Number(template[1]) : undefined),
    enabled: place === 'wizard' && Boolean(template?.[0]),
  }).data?.title;
  const name = detail?.name ?? model.basics.name ?? 'This factory';
  const allPass =
    validation !== undefined && errors.length === 0 && validation.checks.every((c) => c.status === 'passed');

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <h2 data-route-focus="" tabIndex={-1} className="m-0 text-page-title tracking-display">
          {hasDraft ? `Publish v${next}` : 'Review and publish'}
        </h2>
        {hasDraft && changes !== undefined ? <Chip tone="plain">{plural(changes, 'change')}</Chip> : null}
        {validation && errors.length > 0 ? (
          <Chip tone="rework">{plural(errors.length, 'problem')}</Chip>
        ) : allPass ? (
          <Chip tone="ok">All checks pass</Chip>
        ) : null}
      </div>
      <ProblemAlert errors={errors} next={next} model={model} />
      <div className="grid min-w-0 gap-4 @[960px]:grid-cols-[minmax(0,1fr)_360px] @[960px]:items-start">
        <div className="flex min-w-0 flex-col gap-4">
          {pair ? (
            <ChangesCard
              pair={pair}
              fromName={pair.from === 'source' && templateTitle ? `the ${templateTitle} template` : undefined}
              countsOnly={place === 'wizard' && draft?.source.kind === 'blank'}
            />
          ) : (
            <SectionCard title="Changes">
              <p className="m-0 text-secondary text-muted">
                {current
                  ? `v${current.number} runs as it is. Compare two versions in the history, or make a change to open a draft.`
                  : 'Nothing published yet.'}
              </p>
            </SectionCard>
          )}
          {hasDraft && current ? (
            <SectionCard title="What happens to work in progress">
              <p className="m-0 text-body text-ink-soft">
                Runs already started finish on v{current.number}. Every run that starts after you publish uses v{next},
                so an item can change version between stations. Nothing restarts.
              </p>
            </SectionCard>
          ) : null}
        </div>
        <div className="flex min-w-0 flex-col gap-4">
          <PublishCard
            place={place}
            validation={validation}
            moves={moves}
            onMoves={setMoves}
            needs={movesFromProblems(validation?.problems ?? [])}
          />
          {hasDraft ? <ChecksCard validation={validation} model={model} /> : null}
          {place === 'wizard' ? (
            <SectionCard title="Versions">
              <ul className="m-0 list-none p-0">
                <DraftRow label={`Draft of v${next}`} detail={draftDetail(draft?.updated_at)} />
                <li className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-3 border-t border-line py-3">
                  <Chip tone="plain">v1</Chip>
                  <span className="text-body text-muted">Created when you publish</span>
                </li>
                {draft ? (
                  <li className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-3 border-t border-line py-3">
                    <Chip tone="plain">Source</Chip>
                    <span className="text-body text-muted">{sourceText(draft.source, templateTitle)}</span>
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

/** The last four versions, and a link to all of them in the Versions section. */
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
    <SectionCard title="Versions">
      <ul className="m-0 list-none p-0 [&>li:first-child]:border-t-0">
        {hasDraft ? <DraftRow label={`Draft of v${next}`} detail={draftDetail(draftUpdated)} /> : null}
        {versions.slice(0, 4).map((v) => (
          <VersionRow key={v.id} version={v} comparing={compare} onRestore={setRestoring} />
        ))}
      </ul>
      <SectionLink
        section="versions"
        patch={{}}
        className="block border-t border-line py-3 text-secondary font-medium underline underline-offset-3 transition-colors duration-200 hover:text-run"
      >
        All {versions.length} versions
      </SectionLink>
      <RestoreDialog version={restoring} onClose={() => setRestoring(null)} />
    </SectionCard>
  );
}

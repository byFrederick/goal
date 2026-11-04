// After publish (jonah.jpg): each published change's metric before and after at its station, with the verdict
// of its watch (ADR 0039 decisions 14 and 15). A Worse watch of a Jonah proposal offers Undo this change, which
// writes the proposal that reverses it and opens it for a person to approve. The watches come with the
// factory's published versions, newest first; a person's own publish has no proposal to undo.
import { useQuery } from '@tanstack/react-query';
import { proposalsQuery, versionsQuery } from '../../api/queries';
import type { Version, Watch } from '../../api/types';
import { useFactory } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { SectionCard } from '../../ui/card';
import { Chip } from '../../ui/chip';
import { EmptyState } from '../../ui/empty-state';
import { Skeleton } from '../../ui/skeleton';
import { InfoTip } from './info-tip';
import { watchText, watchVerdicts } from './jonah-format';
import { UndoThisChange } from './undo-button';

const block = 'rounded-control bg-hover';

/** The panel's layout in muted blocks: three rows of a change, its numbers and its verdict. */
export function AfterPublishShape() {
  return (
    <div className="flex flex-col gap-4 rounded-card border border-line bg-card p-4 md:p-5">
      <div className={`${block} h-5 w-40`} />
      <div className={`${block} h-12`} />
      <div className={`${block} h-12`} />
      <div className={`${block} h-12`} />
    </div>
  );
}

interface Row {
  key: string;
  version: Version;
  watch: Watch;
}

/** The watches of the versions, newest version first, one row each. */
function rowsOf(versions: readonly Version[]): Row[] {
  return versions.flatMap((version) =>
    (version.watches ?? []).map((watch) => ({
      key: `${version.id}:${watch.station}:${watch.metric}`,
      version,
      watch,
    })),
  );
}

export function AfterPublishPanel() {
  const org = useOrg();
  const factory = useFactory();
  const versions = useQuery(versionsQuery(org.id, factory.id));
  const proposals = useQuery(proposalsQuery(org.id, factory.id));
  const canEdit = factory.can.includes('definition:edit');
  const rows = versions.data ? rowsOf(versions.data.versions) : [];
  const titleOf = (row: Row) => {
    const number = row.watch.proposal;
    if (number === undefined) return row.version.message;
    return proposals.data?.proposals.find((p) => p.number === number)?.title ?? `Proposal ${number}`;
  };
  const sourceOf = (row: Row) => {
    const v = `v${row.version.number}`;
    if (row.watch.proposal !== undefined) return `Proposal ${row.watch.proposal}, ${v}`;
    return row.version.published_by ? `${v}, by ${row.version.published_by.name}` : v;
  };

  return (
    <Skeleton ready={!versions.isPending} placeholder={<AfterPublishShape />}>
      <SectionCard
        title={
          <span className="inline-flex items-center gap-1">
            After publish
            <InfoTip label="How After publish works">
              Each change is watched at its station until its numbers are clear. Worse means they got worse.
            </InfoTip>
          </span>
        }
      >
        {versions.isError ? (
          <EmptyState title="The published changes could not be loaded">
            goald is not answering. Try again in a moment.
          </EmptyState>
        ) : rows.length === 0 ? (
          <p className="m-0 text-secondary text-muted">Nothing published is watched yet.</p>
        ) : (
          <ul className="m-0 flex list-none flex-col p-0">
            {rows.map((row) => {
              const verdict = watchVerdicts[row.watch.verdict];
              const undoable = row.watch.verdict === 'worse' && row.watch.proposal !== undefined;
              return (
                <li
                  key={row.key}
                  className="grid items-center gap-x-4 gap-y-2 border-t border-line py-3 first:border-t-0 md:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)_auto]"
                >
                  <span className="min-w-0">
                    <span className="block font-control text-ink break-words">{titleOf(row)}</span>
                    <span className="block text-secondary text-muted">{sourceOf(row)}</span>
                  </span>
                  <span className="min-w-0">
                    <span className="block text-ink-soft break-words">{watchText(row.watch)}</span>
                    <span className="block text-secondary text-muted">{row.watch.after.items} items</span>
                  </span>
                  <span className="flex flex-wrap items-center gap-2 md:justify-end">
                    <Chip tone={verdict.tone}>{verdict.label}</Chip>
                    {undoable && canEdit && row.watch.proposal !== undefined ? (
                      <UndoThisChange number={row.watch.proposal} factoryId={factory.id} factoryName={factory.name} />
                    ) : null}
                  </span>
                </li>
              );
            })}
          </ul>
        )}
      </SectionCard>
    </Skeleton>
  );
}

// The waits an item is in, shown two ways: WaitLine is the board's one quiet line (its full list is the
// tooltip), WaitingFor the item page's Waiting section with each need, its paths and each ref a link.
import { Link } from '@tanstack/react-router';
import type { ItemWaiting } from '../api/types';
import { cn } from '../ui/cn';
import { InfoTip } from '../ui/info-tip';
import { StatusIcon, type StatusKind } from '../ui/status-icon';
import { CIWait } from './ci-wait';
import { refParam } from './factory';
import { useOrg } from './org';
import { turnTarget, waitingDetail, waitingShort } from './waiting';

/** The board's one wait line: a muted status icon and "Waits for sre#3 +2", the full list as its tooltip. */
export function WaitLine({
  waiting,
  status,
  className,
}: {
  waiting: ItemWaiting;
  status: StatusKind | null;
  className?: string;
}) {
  if (waiting.kind === 'ci') {
    return (
      <span className={cn('flex min-w-0 items-center gap-2 text-muted', className)}>
        {status ? <StatusIcon status={status} className="text-muted" /> : null}
        <CIWait waiting={waiting} card />
      </span>
    );
  }
  return (
    <span title={waitingDetail(waiting)} className={cn('flex min-w-0 items-center gap-1.5 text-muted', className)}>
      {status ? <StatusIcon status={status} className="text-muted" /> : null}
      <span className="truncate" title={waitingDetail(waiting)}>
        {waitingShort(waiting)}
      </span>
    </span>
  );
}

/** The item page's Waiting section: one entry per open need, its paths, and the shared files of a files wait. */
export function WaitingFor({ waiting }: { waiting: ItemWaiting | null | undefined }) {
  const org = useOrg();
  if (!waiting || waiting.needs.length === 0) return null;
  const sameFiles = waiting.kind === 'files';
  const turn = waiting.kind === 'turn';
  return (
    <section aria-labelledby="waiting-heading" className="flex max-w-[820px] min-w-0 flex-col gap-2">
      <div className="flex items-center gap-1">
        <h2 id="waiting-heading" className="m-0 text-sm font-medium text-muted">
          Waiting
        </h2>
        {turn ? (
          <InfoTip about="Waiting">
            This station merges one item into a branch at a time, so each pull request is tested against the latest
            branch. This item starts when the one ahead is merged or sent back.
          </InfoTip>
        ) : null}
      </div>
      <ul className="m-0 flex list-none flex-col gap-2 p-0 text-sm text-ink">
        {waiting.needs.map(({ ref, title, paths }) => {
          // The ref opens after a fixed prefix that holds no "#", so its last "#" is its place.
          const factory = ref.slice(0, ref.lastIndexOf('#'));
          return (
            <li key={ref} className="flex min-w-0 flex-col gap-0.5">
              <span className="break-words">
                {sameFiles ? 'Same files as ' : turn ? 'Waiting for ' : 'Waits for '}
                <Link
                  to="/o/$org/f/$factory/items/$ref"
                  params={{ org: org.slug, factory, ref: refParam(ref) }}
                  className="font-medium text-ink underline-offset-2 hover:underline"
                >
                  {ref}
                </Link>
                {turn ? ` to land on ${turnTarget(waiting)}` : ''} {title}
              </span>
              {paths.length > 0 ? (
                <span className="font-mono text-xs break-words text-muted">{paths.join(', ')}</span>
              ) : null}
            </li>
          );
        })}
      </ul>
      {sameFiles && waiting.files.length > 0 ? (
        <p className="m-0 text-sm break-words text-muted">Shared files: {waiting.files.join(', ')}</p>
      ) : null}
    </section>
  );
}

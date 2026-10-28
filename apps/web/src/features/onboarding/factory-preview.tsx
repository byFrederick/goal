// The dark panel's cards on steps 3 and 4 (docs/design/screens/get-started/template.jpg and line.jpg): the
// organization and code cards from steps 1 and 2, then the factory card with its line. The line is the
// andon station nodes joined by a rule, each with its WIP slots as small squares. The factory card reads
// the draft (step 4) or the picked template (step 3, before the factory exists).
import type { ReactNode } from 'react';
import type { OrgSummary } from '../../api/types';
import type { StationKind } from '../../shared/definition/model';
import { formatUsd } from '../../shared/format';
import { cn } from '../../ui/cn';
import { GitHubMark } from '../../ui/github-mark';
import { StationNode, type StationNodeTone } from '../../ui/station-node';
import { PreviewCard, type PreviewSlots } from './onboarding-layout';

/** A station as the panel draws it: its name, its kind (Done shows a check) and its WIP limit. */
export interface PreviewStation {
  id: string;
  label: string;
  kind: StationKind;
  wip?: number | undefined;
}

/** WIP slots drawn past this count are left out; the number is still said for screen readers. */
const maxSlots = 12;

/** The organization card, first in the grid. */
export function organizationCard(org: OrgSummary, host: string): ReactNode {
  return <PreviewCard label="Organization" value={org.name} detail={`${host}/o/${org.slug}`} />;
}

/** The code card: the first repository chosen in step 2. */
export function codeCard(repo: string | undefined): ReactNode {
  if (repo === undefined) return undefined;
  return (
    <PreviewCard
      active
      label="Code"
      value={repo}
      icon={<GitHubMark className="shrink-0" />}
      detail="GitHub, read and pull requests"
    />
  );
}

/** The panel's slots for a step: the organization, the code and the factory card; the rest stay dashed. */
export function previewSlots({
  organization,
  code,
  factory,
}: {
  organization: ReactNode;
  code: ReactNode;
  factory: ReactNode;
}): PreviewSlots {
  return { organization, second: code, line: factory };
}

/** The factory's initial in a blue square: the factory's avatar (the name field and the card). */
export function FactoryInitial({ name, className }: { name: string; className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        'flex size-9 shrink-0 items-center justify-center rounded-control bg-run font-control text-body text-white',
        className,
      )}
    >
      {name.trim().charAt(0).toUpperCase() || '·'}
    </span>
  );
}

/** The factory card: its initial, name and purpose, then the line. `item` marks the station the first item is at. */
export function FactoryCard({
  name,
  purpose,
  stations,
  item,
}: {
  name: string;
  purpose: string;
  stations: readonly PreviewStation[];
  item?: { station: string; number: number } | undefined;
}) {
  return (
    <section
      aria-label="Your factory"
      className="flex min-w-0 flex-col gap-5 rounded-card border border-run-soft bg-andon-raised p-4 ring-4 ring-run-soft/25 md:p-5"
    >
      <div className="flex min-w-0 items-center gap-3">
        <FactoryInitial name={name} />
        <div className="flex min-w-0 flex-col">
          <span className="font-control text-heading break-words text-white">{name || 'Your factory'}</span>
          {purpose ? <span className="text-secondary break-words text-andon-text">{purpose}</span> : null}
        </div>
      </div>
      <FactoryLine stations={stations} item={item} />
    </section>
  );
}

/** The stations in order, joined by a rule. The last station with kind `done` shows a check. */
function FactoryLine({
  stations,
  item,
}: {
  stations: readonly PreviewStation[];
  item: { station: string; number: number } | undefined;
}) {
  if (stations.length === 0) {
    return <p className="m-0 text-secondary text-andon-text">The stations appear here as you add them.</p>;
  }
  return (
    <ol aria-label="The line" className="m-0 flex list-none gap-1 p-0">
      {stations.map((s, i) => {
        const first = i === 0;
        const last = i === stations.length - 1;
        const tone: StationNodeTone = s.kind === 'done' ? 'done' : 'plain';
        return (
          <li key={s.id} className="flex min-w-0 flex-1 flex-col items-center gap-2">
            <div className="flex w-full items-center">
              <span aria-hidden="true" className={cn('h-0.5 flex-1', first ? 'bg-transparent' : 'bg-run')} />
              <span className="relative flex">
                <StationNode tone={tone} size="compact" className="size-9 border-2 md:size-11" />
                {item?.station === s.id ? (
                  <span
                    aria-hidden="true"
                    className="absolute -top-1.5 -right-1.5 flex h-4 min-w-4 items-center justify-center rounded-chip bg-amber px-1 text-meta font-control text-ink"
                  >
                    {item.number}
                  </span>
                ) : null}
              </span>
              <span aria-hidden="true" className={cn('h-0.5 flex-1', last ? 'bg-transparent' : 'bg-run')} />
            </div>
            <span className="max-w-full truncate text-secondary text-andon-text">{s.label}</span>
            <WipSlots wip={s.wip} label={s.label} />
          </li>
        );
      })}
    </ol>
  );
}

/** One small square per WIP slot of the station. */
function WipSlots({ wip, label }: { wip: number | undefined; label: string }) {
  if (!wip || wip < 1) return null;
  const slots = Array.from({ length: Math.min(wip, maxSlots) }, (_, n) => `slot-${n + 1}`);
  return (
    <span className="flex flex-wrap justify-center gap-1">
      <span className="sr-only">{`WIP limit ${wip} at ${label}`}</span>
      {slots.map((slot) => (
        <span key={slot} aria-hidden="true" className="size-2 rounded-xs border border-andon-line" />
      ))}
    </span>
  );
}

/** The running item under the panel's cards (step 8): its number, its title, and where it is and what it has cost. */
export function RunningItemCard({
  number,
  title,
  station,
  minutes,
  cost,
}: {
  number: number;
  title: string;
  station: string;
  /** Minutes since the item was created. */
  minutes: number;
  cost: number;
}) {
  return (
    <section
      aria-label="Your first item"
      className="flex min-w-0 items-center gap-3 rounded-card border border-run-soft bg-andon-raised p-4 ring-4 ring-run-soft/25"
    >
      <span
        aria-hidden="true"
        className="flex size-7 flex-none items-center justify-center rounded-control bg-amber font-control text-secondary text-ink"
      >
        {number}
      </span>
      <div className="flex min-w-0 flex-col">
        <span className="font-control text-body break-words text-white">{title}</span>
        <span className="text-secondary break-words text-andon-text">
          {`At ${station}, ${minutes} min in, ${formatUsd(cost)} so far`}
        </span>
      </div>
    </section>
  );
}

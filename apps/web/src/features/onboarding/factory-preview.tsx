// The dark panel's cards on steps 3 and 4 (docs/design/screens/get-started/template.jpg and line.jpg): the
// organization and code cards from steps 1 and 2, then the factory card with its line. The line is the
// andon station nodes joined by a rule, each with its WIP slots as small squares. The factory card reads
// the draft (step 4) or the picked template (step 3, before the factory exists).

import { useQuery } from '@tanstack/react-query';
import { GitBranch } from 'lucide-react';
import type { ReactNode } from 'react';
import { draftModelQuery, factoryQuery } from '../../api/queries';
import type { OrgSummary } from '../../api/types';
import { modelOf, type StationKind } from '../../shared/definition/model';
import { factoryTitle } from '../../shared/factory';
import { formatUsd } from '../../shared/format';
import { cn } from '../../ui/cn';
import { GitHubMark } from '../../ui/github-mark';
import { GitLabMark } from '../../ui/gitlab-mark';
import { Skeleton } from '../../ui/skeleton';
import { StationNode, type StationNodeTone } from '../../ui/station-node';
import { stationLabel } from '../factory-editor/line/labels';
import { repoOf } from './code-search';
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

/** The mark of a repository's host: GitHub's, GitLab's, or a neutral branch icon for any other host. */
function HostMark({ url }: { url: string | undefined }) {
  let host = '';
  try {
    host = url ? new URL(url).hostname.toLowerCase() : '';
  } catch {
    host = '';
  }
  if (host === 'github.com') return <GitHubMark className="shrink-0" />;
  if (host === 'gitlab.com' || host.startsWith('gitlab.')) return <GitLabMark className="shrink-0" />;
  return <GitBranch aria-hidden="true" className="size-4 shrink-0" />;
}

/**
 * The code card. With a repository it shows the repository and its host's mark. Without one, it shows "No code"
 * once the Code step is done (`done`), and nothing before that: the slot stays dashed. The card fades in over
 * the dashed slot (MOTION.md, Loading).
 */
export function codeCard({
  url,
  repo,
  secret,
  done = false,
}: {
  url?: string | undefined;
  /** The label to show; defaults to the one read from `url`. */
  repo?: string | undefined;
  secret?: string | undefined;
  done?: boolean;
}): ReactNode {
  const label = repo ?? repoOf(url);
  const reveal = 'motion-safe:animate-slot-reveal';
  if (label === undefined) {
    if (!done) return undefined;
    return (
      <PreviewCard active label="Code" value="No code" detail="Items don't touch a repository" className={reveal} />
    );
  }
  return (
    <PreviewCard
      active
      label="Code"
      value={label}
      icon={<HostMark url={url} />}
      detail={secret ? `Token ${secret}` : 'Public repository'}
      className={reveal}
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
      className="flex min-w-0 flex-col gap-5 rounded-card border border-run bg-hover p-4 ring-4 ring-run/25 md:p-5"
    >
      <div className="flex min-w-0 items-center gap-3">
        <FactoryInitial name={name} />
        <div className="flex min-w-0 flex-col">
          <span className="font-control text-heading break-words text-ink">{name || 'Your factory'}</span>
          {purpose ? <span className="text-secondary break-words text-ink-soft">{purpose}</span> : null}
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
    return <p className="m-0 text-secondary text-ink-soft">The stations appear here as you add them.</p>;
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
            <span className="max-w-full truncate text-secondary text-ink-soft">{s.label}</span>
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
        <span key={slot} aria-hidden="true" className="size-2 rounded-chip border border-muted" />
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
      className="flex min-w-0 items-center gap-3 rounded-card border border-run bg-hover p-4 ring-4 ring-run/25"
    >
      <span
        aria-hidden="true"
        className="flex size-7 flex-none items-center justify-center rounded-control bg-amber font-control text-secondary text-ink"
      >
        {number}
      </span>
      <div className="flex min-w-0 flex-col">
        <span className="font-control text-body break-words text-ink">{title}</span>
        <span className="text-secondary break-words text-ink-soft">
          {`At ${station}, ${minutes} min in, ${formatUsd(cost)} so far`}
        </span>
      </div>
    </section>
  );
}

/**
 * The factory card of a step before the line is open for editing (the Code step): the factory's name and the
 * stations of its draft. Until both load it shows the card as muted blocks; without a draft, the card has no stations.
 */
export function FactoryCardOf({ org, factory }: { org: string; factory: string }) {
  const detail = useQuery(factoryQuery(org, factory));
  const model = useQuery(draftModelQuery(org, factory));
  const ready = !detail.isPending && !model.isPending;
  const { line, basics } = model.data ? modelOf(model.data.model) : { line: undefined, basics: undefined };
  const stations: PreviewStation[] = (line?.stations ?? []).map((s) => ({
    id: s.id,
    label: stationLabel(s),
    kind: s.kind,
    wip: s.wip,
  }));
  return (
    <Skeleton ready={ready} placeholder={<FactoryCardPlaceholder />}>
      <FactoryCard
        name={detail.data ? factoryTitle(detail.data) : ''}
        purpose={basics?.purpose ?? ''}
        stations={stations}
      />
    </Skeleton>
  );
}

function FactoryCardPlaceholder() {
  return (
    <div className="flex min-h-42 flex-col gap-5 rounded-card border border-line bg-hover p-4 md:p-5">
      <div className="flex items-center gap-3">
        <div className="size-9 rounded-control bg-line" />
        <div className="h-4 w-2/5 rounded-control bg-line" />
      </div>
      <div className="h-8 rounded-control bg-line" />
    </div>
  );
}

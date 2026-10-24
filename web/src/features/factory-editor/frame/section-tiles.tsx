// The section tiles (D §10.3): one link per section with its state ("1 change, 1 problem", "2
// changes", "No changes") and Review and publish with what stands before a publish. A grid of
// five once `main` is wide enough, else a row that scrolls inside `main` (never the page).
import { useLinkProps } from '@tanstack/react-router';
import { Pencil, TriangleAlert, Upload } from 'lucide-react';
import { type ReactNode, useEffect, useRef } from 'react';
import type { SectionSummary } from '../../../api/types';
import { type Section, sections } from '../../../shared/definition/model';
import { useFactoryParams } from '../../../shared/factory';
import { cn } from '../../../ui/cn';
import { type EditorSection, sectionInfo } from '../sections';
import { reviewTile, sectionTile } from './state';

interface TilesProps {
  current: EditorSection;
  summaries: readonly SectionSummary[];
  changeCount: number;
  problemCount: number;
  hasDraft: boolean;
}

export function SectionTiles({ current, summaries, changeCount, problemCount, hasDraft }: TilesProps) {
  return (
    <nav aria-label="Definition sections" className="-mx-4 min-w-0 md:mx-0">
      <ul className="m-0 flex list-none gap-2 overflow-x-auto px-4 py-0.5 md:px-0 @[880px]:grid @[880px]:grid-cols-5 @[880px]:overflow-visible">
        {sections.map((s) => {
          const tile =
            s === 'review'
              ? reviewTile({ problems: problemCount, changes: changeCount, hasDraft })
              : sectionTile(summaries.find((x) => x.section === s));
          return (
            <li key={s} className="min-w-[176px] shrink-0 @[880px]:min-w-0">
              <Tile section={s} current={current === s} problem={tile.state === 'problem'} text={tile.text}>
                <TileIcon section={s} state={tile.state} />
              </Tile>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}

function Tile({
  section,
  current,
  problem,
  text,
  children,
}: {
  section: Section;
  current: boolean;
  problem: boolean;
  text: string;
  children: ReactNode;
}) {
  const params = useFactoryParams();
  // A tile is the current location in the editor (aria-current="location"), not a page.
  const { 'aria-current': _page, ...link } = useLinkProps({
    to: '/o/$org/f/$factory/definition/$section',
    params: { ...params, section },
  });
  const label = sectionInfo[section].label;
  // In the scrolling row (phones), the current tile scrolls into view.
  const ref = useRef<HTMLAnchorElement>(null);
  useEffect(() => {
    if (current) ref.current?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }, [current]);
  return (
    <a
      {...link}
      ref={ref}
      href={link.href}
      aria-current={current ? 'location' : undefined}
      className={cn(
        // relative: the screen-reader name stays inside the tile, not past the scrolling row.
        'relative flex min-h-11 items-center gap-2.5 rounded-control border px-3 py-1.5 text-sm no-underline transition-colors duration-200',
        current
          ? 'border-ink bg-ink text-white'
          : 'border-line bg-card text-ink hover:border-rule-strong hover:bg-surface',
      )}
    >
      {children}
      {/* One name for screen readers ("Connections, 1 change, 1 problem"); two lines on screen. */}
      <span className="sr-only">{`${label}, ${text}`}</span>
      <span aria-hidden="true" className="flex min-w-0 flex-col leading-tight">
        <span className="truncate font-medium">{label}</span>
        <span
          className={cn(
            'truncate text-xs',
            current ? (problem ? 'text-rework-line' : 'text-white/80') : problem ? 'text-rework-ink' : 'text-muted',
          )}
        >
          {text}
        </span>
      </span>
    </a>
  );
}

const iconBox = 'flex size-[22px] shrink-0 items-center justify-center rounded-chip';

function TileIcon({ section, state }: { section: Section; state: string }) {
  if (section === 'review') {
    return (
      <span aria-hidden="true" className={cn(iconBox, 'bg-control text-ink')}>
        <Upload size={13} strokeWidth={1.75} />
      </span>
    );
  }
  if (state === 'problem') {
    return (
      <span aria-hidden="true" className={cn(iconBox, 'bg-rework-tint text-rework')}>
        <TriangleAlert size={14} strokeWidth={1.75} />
      </span>
    );
  }
  if (state === 'edited') {
    return (
      <span aria-hidden="true" className={cn(iconBox, 'bg-control text-ink')}>
        <Pencil size={13} strokeWidth={1.75} />
      </span>
    );
  }
  return <span aria-hidden="true" className="mx-0.5 size-[18px] shrink-0 rounded-full border-[1.5px] border-target" />;
}

// The section tiles (D §10.3): one link per section with its state ("1 change, 1 problem", "2
// changes", "No changes") and Review and publish with what stands before a publish. A grid of
// five once `main` is wide enough, else a row that scrolls inside `main` (never the page). In
// YAML mode each tile jumps to its block of factory.yaml ("board, lines 31–45", D §10.9); while
// factory.yaml does not parse, every tile says so. View only, there is no Review and publish and
// each tile says what its section holds, with an eye (canvas Define-View).
import { useLinkProps } from '@tanstack/react-router';
import { Eye, Pencil, TriangleAlert, Upload } from 'lucide-react';
import { type ReactNode, useEffect, useRef } from 'react';
import type { ParseError, SectionSummary } from '../../../api/types';
import { type Section, sections } from '../../../shared/definition/model';
import { useFactoryParams } from '../../../shared/factory';
import { cn } from '../../../ui/cn';
import { useModel } from '../draft/use-draft';
import { type EditorSection, sectionInfo } from '../sections';
import { parseErrorTile, reviewTile, sectionTile, viewTileText, yamlTileText } from './state';

interface TilesProps {
  current: EditorSection;
  summaries: readonly SectionSummary[];
  changeCount: number;
  problemCount: number;
  hasDraft: boolean;
  /** The YAML view is open: tiles jump to their block. */
  yaml: boolean;
  parseError: ParseError | null;
  /** The caller may not change the definition. */
  viewOnly?: boolean;
}

export function SectionTiles({
  current,
  summaries,
  changeCount,
  problemCount,
  hasDraft,
  yaml,
  parseError,
  viewOnly = false,
}: TilesProps) {
  const model = useModel();
  const shown = viewOnly ? sections.filter((s) => s !== 'review') : sections;
  return (
    <nav
      aria-label={yaml ? 'Definition sections, jump to a block in factory.yaml' : 'Definition sections'}
      className="-mx-4 min-w-0 md:mx-0"
    >
      <ul
        className={cn(
          'm-0 flex list-none gap-2 overflow-x-auto px-4 py-0.5 md:px-0 @[880px]:grid @[880px]:overflow-visible',
          viewOnly ? '@[880px]:grid-cols-4' : '@[880px]:grid-cols-5',
        )}
      >
        {shown.map((s) => {
          const summary = summaries.find((x) => x.section === s);
          const tile = parseError
            ? parseErrorTile(s, parseError)
            : s === 'review'
              ? reviewTile({ problems: problemCount, changes: changeCount, hasDraft })
              : viewOnly
                ? { state: 'view' as const, text: viewTileText(s, model) }
                : sectionTile(summary);
          const line = yaml ? summary?.lines[0] : undefined;
          return (
            <li key={s} className="min-w-[176px] shrink-0 @[880px]:min-w-0">
              <Tile
                section={s}
                current={current === s}
                problem={tile.state === 'problem'}
                text={yaml ? yamlTileText(s, summary?.lines) : tile.text}
                state={yaml ? tile.text : undefined}
                line={yaml ? (line && line > 0 ? line : null) : undefined}
              >
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
  state,
  line,
  children,
}: {
  section: Section;
  current: boolean;
  problem: boolean;
  text: string;
  /** In YAML mode the state is spoken after the block ("…, 2 changes"). */
  state?: string | undefined;
  /** In YAML mode: the block's first line (null when the block is not in the file yet). */
  line?: number | null | undefined;
  children: ReactNode;
}) {
  const params = useFactoryParams();
  // A tile is the current location in the editor (aria-current="location"), not a page.
  const { 'aria-current': _page, ...link } = useLinkProps({
    to: '/o/$org/f/$factory/definition/$section',
    params: { ...params, section },
    search: line === undefined ? {} : { mode: 'yaml' as const, ...(line ? { line } : {}) },
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
        current ? 'border-ink bg-ink text-white' : 'border-line bg-card text-ink hover:border-rule hover:bg-card',
      )}
    >
      {children}
      {/* One name for screen readers ("Connections, 1 change, 1 problem"); two lines on screen. */}
      <span className="sr-only">{state ? `${label}, ${text}, ${state}` : `${label}, ${text}`}</span>
      <span aria-hidden="true" className="flex min-w-0 flex-col leading-tight">
        <span className="truncate font-medium">{label}</span>
        <span
          className={cn(
            'truncate text-xs',
            current ? (problem ? 'text-rework-tint' : 'text-white/80') : problem ? 'text-rework-ink' : 'text-muted',
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
  if (state === 'view') {
    return (
      <span aria-hidden="true" className={cn(iconBox, 'bg-panel text-ink')}>
        <Eye size={13} strokeWidth={1.75} />
      </span>
    );
  }
  if (section === 'review') {
    return (
      <span aria-hidden="true" className={cn(iconBox, 'bg-panel text-ink')}>
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
      <span aria-hidden="true" className={cn(iconBox, 'bg-panel text-ink')}>
        <Pencil size={13} strokeWidth={1.75} />
      </span>
    );
  }
  return <span aria-hidden="true" className="mx-0.5 size-[18px] shrink-0 rounded-full border-[1.5px] border-muted" />;
}

// The section tiles (D §10.3): one link per section with its state ("1 change, 1 problem", "2
// changes", "No changes") and Review and publish with what stands before a publish. A grid of
// five once `main` is wide enough, else a row that scrolls inside `main` (never the page). In
// YAML mode each tile jumps to its block of factory.yaml ("board, lines 31–45", D §10.9); while
// factory.yaml does not parse, every tile says so. View only, there is no Review and publish and
// each tile says what its section holds, with an eye (canvas Define-View).
import { useLinkProps } from '@tanstack/react-router';
import { useEffect, useRef } from 'react';
import type { ParseError, SectionSummary } from '../../../api/types';
import { type Section, sections } from '../../../shared/definition/model';
import { useFactoryParams } from '../../../shared/factory';
import { cn } from '../../../ui/cn';
import { useModel } from '../draft/use-draft';
import { useEnvironmentNames } from '../environments/use-environments';
import { type EditorSection, routeSection, sectionInfo } from '../sections';
import { parseErrorTile, reviewTile, sectionTile, viewTileText, yamlTileText } from './state';

interface TilesProps {
  current: EditorSection;
  summaries: readonly SectionSummary[];
  changeCount: number;
  problemCount: number;
  hasDraft: boolean;
  /** The version number the draft started from. */
  base?: number | null;
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
  base,
  yaml,
  parseError,
  viewOnly = false,
}: TilesProps) {
  const model = useModel();
  const envs = useEnvironmentNames();
  const shown = viewOnly ? sections.filter((s) => s !== 'review') : sections;
  return (
    <nav
      aria-label={yaml ? 'Definition sections, jump to a block in factory.yaml' : 'Definition sections'}
      className="-mx-4 min-w-0 md:mx-0"
    >
      <ul
        className={cn(
          'm-0 flex list-none gap-2 overflow-x-auto px-4 py-0.5 md:px-0 @[880px]:flex-col @[880px]:gap-1 @[880px]:overflow-visible',
        )}
      >
        {shown.map((s) => {
          const summary = summaries.find((x) => x.section === s);
          const tile: { state: string; text: string; badge?: string } = parseError
            ? parseErrorTile(s, parseError)
            : s === 'review'
              ? reviewTile({ problems: problemCount, changes: changeCount, hasDraft, base })
              : viewOnly
                ? { state: 'view' as const, text: viewTileText(s, model) }
                : sectionTile(summary, s, model);
          const badge = tile.badge;
          const line = yaml ? summary?.lines[0] : undefined;
          return (
            <li key={s} className="min-w-[176px] shrink-0 @[880px]:min-w-0">
              <Tile
                section={s}
                current={current === routeSection(s)}
                problem={tile.state === 'problem'}
                badge={yaml ? undefined : badge}
                text={
                  yaml
                    ? yamlTileText(s, summary?.lines)
                    : s === 'connections' && envs.ready
                      ? envs.names.join(', ') || 'None yet'
                      : tile.text
                }
                state={yaml ? tile.text : undefined}
                line={yaml ? (line && line > 0 ? line : null) : undefined}
              />
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
  badge,
  text,
  state,
  line,
}: {
  section: Section;
  current: boolean;
  problem: boolean;
  /** What needs attention, beside the label ("2 changes"). */
  badge?: string | undefined;
  text: string;
  /** In YAML mode the state is spoken after the block ("…, 2 changes"). */
  state?: string | undefined;
  /** In YAML mode: the block's first line (null when the block is not in the file yet). */
  line?: number | null | undefined;
}) {
  const params = useFactoryParams();
  // A tile is the current location in the editor (aria-current="location"), not a page.
  const { 'aria-current': _page, ...link } = useLinkProps({
    to: '/o/$org/f/$factory/definition/$section',
    params: { ...params, section: routeSection(section) },
    search: line === undefined ? {} : { mode: 'yaml' as const, ...(line ? { line } : {}) },
  });
  const label = sectionInfo[section].label;
  // In the scrolling row (phones), the current tile scrolls into view.
  const ref = useRef<HTMLAnchorElement>(null);
  useEffect(() => {
    if (current) ref.current?.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: 'auto' });
  }, [current]);
  return (
    <a
      {...link}
      ref={ref}
      href={link.href}
      aria-current={current ? 'location' : undefined}
      className={cn(
        // relative: the screen-reader name stays inside the tile, not past the scrolling row.
        'relative flex min-h-11 items-center gap-2.5 rounded-control border px-3 py-1.5 text-sm no-underline transition-colors duration-(--duration-quick)',
        current ? 'border-transparent bg-hover text-ink' : 'border-transparent text-ink hover:bg-hover',
      )}
    >
      {/* One name for screen readers ("Connections, 1 change, 1 problem"); two lines on screen. */}
      <span className="sr-only">{[label, badge, text, state].filter(Boolean).join(', ')}</span>
      <span aria-hidden="true" className="flex min-w-0 flex-1 flex-col leading-tight">
        <span className="flex items-baseline justify-between gap-2">
          <span className="truncate font-medium" title={label}>
            {label}
          </span>
          {badge ? (
            <span className={cn('shrink-0 text-xs', problem ? 'text-rework-ink' : 'text-muted')}>{badge}</span>
          ) : null}
        </span>
        <span className={cn('truncate text-xs', problem && !badge ? 'text-rework-ink' : 'text-muted')} title={text}>
          {text}
        </span>
      </span>
    </a>
  );
}

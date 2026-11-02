// A connection kind's mark: its brand glyph centred in a bordered square, with no filled tile. A kind
// without a glyph shows its uppercase initial in the same square. Decorative: the name beside it says
// what it is. The Connections list, the add-connection catalog and the Knowledge screens share it.
import type { ComponentType, SVGProps } from 'react';
import { cn } from './cn';
import { GitHubMark } from './github-mark';

const glyphs: Record<string, ComponentType<SVGProps<SVGSVGElement>>> = {
  github: GitHubMark,
};

export function ConnectionGlyph({
  kind,
  title,
  className,
}: {
  /** The kind's name, e.g. "github". */
  kind: string;
  /** The kind's title, for the initial when the kind has no glyph. */
  title?: string | undefined;
  className?: string;
}) {
  const Glyph = glyphs[kind];
  return (
    <span
      aria-hidden="true"
      className={cn(
        'flex size-9 shrink-0 items-center justify-center rounded-control border border-line bg-card text-secondary font-control text-ink',
        className,
      )}
    >
      {Glyph ? <Glyph /> : (title ?? kind).trim().charAt(0).toUpperCase()}
    </span>
  );
}

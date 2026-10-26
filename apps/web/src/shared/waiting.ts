// Why a queued or ready item waits, in words: one line per open need, for the board's waiting rows and
// the item page. Pure: the path summary is the directory the needed item's globs share, or the globs
// themselves when they share none, or the needed item's title when it names no paths.
import type { ItemWaiting } from '../api/types';

export interface WaitingLine {
  /** The needed item's <factory name>#<number>; the text contains it once, where the link goes. */
  ref: string;
  text: string;
}

const wildcard = /[*?[{]/;

/** The directory a glob sits in: the glob cut before its first wildcard, then to its last "/" (`terraform/**` is `terraform/`). */
function directoryOf(glob: string): string {
  const cut = glob.search(wildcard);
  const literal = cut < 0 ? glob : glob.slice(0, cut);
  return literal.slice(0, literal.lastIndexOf('/') + 1);
}

/** The longest directory all the given directories share, ending in "/" or empty. */
function commonDirectory(dirs: string[]): string {
  const [first = '', ...rest] = dirs;
  let end = first.length;
  for (const dir of rest) {
    let i = 0;
    while (i < end && i < dir.length && dir[i] === first[i]) i++;
    end = i;
  }
  return end === 0 ? '' : first.slice(0, first.lastIndexOf('/', end - 1) + 1);
}

/**
 * What a need covers, for its line: the directory its globs share ("terraform/"), else the globs
 * (two or fewer, or the first two and "+N more"), else the title when it names no paths.
 */
export function pathSummary(title: string, paths: string[]): string {
  if (paths.length === 0) return title;
  const dir = commonDirectory(paths.map(directoryOf));
  if (dir) return dir;
  return paths.length <= 2 ? paths.join(', ') : `${paths[0]}, ${paths[1]}, +${paths.length - 2} more`;
}

/**
 * One line per open need of a waiting item: "waits for <ref> (moves <summary>)" for kind after,
 * "waits for the same files as <ref> (<summary>)" for kind files.
 */
export function waitingLines(waiting: Pick<ItemWaiting, 'kind' | 'needs'> | null | undefined): WaitingLine[] {
  if (!waiting) return [];
  return waiting.needs.map(({ ref, title, paths }) => {
    const summary = pathSummary(title, paths);
    return {
      ref,
      text:
        waiting.kind === 'files'
          ? `waits for the same files as ${ref} (${summary})`
          : `waits for ${ref} (moves ${summary})`,
    };
  });
}

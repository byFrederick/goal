// The lines saying which open needs hold an item (waitingLines), one per need, each ref a link to
// that item's page. Shared by the board's waiting rows and the item page.
import { Link } from '@tanstack/react-router';
import type { ItemWaiting } from '../api/types';
import { cn } from '../ui/cn';
import { refParam } from './factory';
import { useOrg } from './org';
import { waitingLines } from './waiting';

export function WaitingFor({ waiting, className }: { waiting: ItemWaiting | null | undefined; className?: string }) {
  const org = useOrg();
  const lines = waitingLines(waiting);
  if (lines.length === 0) return null;
  return (
    <ul className={cn('m-0 flex list-none flex-col gap-0.5 p-0 text-xs text-muted', className)}>
      {lines.map(({ ref, text }) => {
        // The ref opens after a fixed prefix that holds no "#", so its first occurrence is its place.
        const at = text.indexOf(ref);
        const factory = ref.slice(0, ref.lastIndexOf('#'));
        return (
          <li key={ref} title={text} className="flex min-w-0 items-center">
            <span className="min-w-0 truncate">{text.slice(0, at)}</span>
            <Link
              to="/o/$org/f/$factory/items/$ref"
              params={{ org: org.slug, factory, ref: refParam(ref) }}
              className="inline-flex min-h-11 shrink-0 items-center font-medium text-ink underline-offset-2 hover:underline md:min-h-0"
            >
              {ref}
            </Link>
            <span className="min-w-0 truncate">{text.slice(at + ref.length)}</span>
          </li>
        );
      })}
    </ul>
  );
}

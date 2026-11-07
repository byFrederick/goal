// A CI wait as one line: "Waiting for CI · 9 of 14", or "CI failed: task web:e2e (3/12)" after a failure.
// Its commits and checks sit behind an ⓘ. That is a popover, not an InfoTip: it holds links, which a
// tooltip's aria-hidden text cannot, and it stays open when the page scrolls.
import { Info } from 'lucide-react';
import type { KeyboardEvent, PointerEvent } from 'react';
import type { CICheck, ItemWaiting } from '../api/types';
import { cn } from '../ui/cn';
import { Popover, PopoverContent, PopoverTrigger } from '../ui/popover';
import { shortSha, waitingShort } from './waiting';

/** What a check says about itself: Running, Pending, or what it concluded. */
export function checkStatus(check: Pick<CICheck, 'state' | 'result'>): string {
  if (check.state === 'running') return 'Running';
  if (check.state === 'pending') return 'Pending';
  if (check.result === 'failed') return 'Failed';
  if (check.result === 'neutral') return 'Neutral';
  return 'Passed';
}

// A card is a drag handle and a keyboard-movable item: presses on the ⓘ and its panel stay here.
const keep = (e: PointerEvent | KeyboardEvent) => e.stopPropagation();

const link = 'underline-offset-2 hover:underline focus-visible:underline';

function Check({ check }: { check: CICheck }) {
  const status = checkStatus(check);
  const jobs = check.failed_jobs.length > 0 ? ` (${check.failed_jobs.join(', ')})` : '';
  return (
    <li className="flex min-w-0 items-baseline justify-between gap-3">
      <span className="min-w-0 break-words">
        {check.url ? (
          <a href={check.url} target="_blank" rel="noreferrer" className={cn(link, 'text-ink')}>
            {check.name}
          </a>
        ) : (
          check.name
        )}
        {jobs ? <span className="text-muted">{jobs}</span> : null}
      </span>
      <span className={cn('flex-none', status === 'Failed' ? 'text-rework-ink' : 'text-muted')}>{status}</span>
    </li>
  );
}

/** The repositories a CI wait lists: the commits it runs on and the checks of each, with the failed ones. */
function CIList({ ci }: { ci: NonNullable<ItemWaiting['ci']> }) {
  const failed = ci.failure;
  const commits = ci.commits.length > 0 || !failed ? ci.commits : [failed];
  return (
    <div className="flex flex-col gap-3 p-3 text-sm">
      {commits.map((commit) => {
        const checks = ci.checks.filter((c) => c.repo === commit.repo);
        const shown = checks.length > 0 || !failed || failed.repo !== commit.repo ? checks : failed.checks;
        return (
          <fieldset
            key={`${commit.repo}@${commit.sha}`}
            aria-label={commit.repo}
            className="m-0 flex min-w-0 flex-col gap-1 border-0 p-0"
          >
            <div className="flex min-w-0 items-baseline justify-between gap-3">
              <span className="min-w-0 break-all font-medium text-ink">{commit.repo}</span>
              {commit.url ? (
                <a
                  href={commit.url}
                  target="_blank"
                  rel="noreferrer"
                  className={cn(link, 'flex-none font-mono text-xs text-muted')}
                >
                  {shortSha(commit.sha)}
                </a>
              ) : (
                <span className="flex-none font-mono text-xs text-muted">{shortSha(commit.sha)}</span>
              )}
            </div>
            {shown.length > 0 ? (
              <ul className="m-0 flex list-none flex-col gap-1 p-0">
                {shown.map((c) => (
                  <Check key={`${c.name}:${c.url ?? ''}`} check={c} />
                ))}
              </ul>
            ) : (
              <span className="text-muted">No checks yet</span>
            )}
          </fieldset>
        );
      })}
    </div>
  );
}

export function CIWait({ waiting, className }: { waiting: ItemWaiting; className?: string }) {
  const ci = waiting.ci;
  return (
    <span className={cn('flex min-w-0 items-center gap-1', className)}>
      <span className="min-w-0 truncate" title={waitingShort(waiting)}>
        {waitingShort(waiting)}
      </span>
      {ci ? (
        <Popover>
          <PopoverTrigger asChild>
            <button
              type="button"
              aria-label="About CI"
              onPointerDown={keep}
              onKeyDown={keep}
              className="press relative inline-flex size-6 flex-none items-center justify-center rounded-chip text-muted before:absolute before:-inset-1 before:content-[''] max-md:before:-inset-2.5 transition-colors duration-(--duration-quick) hover:text-ink motion-reduce:transition-none"
            >
              <Info size={14} strokeWidth={1.75} aria-hidden="true" />
            </button>
          </PopoverTrigger>
          <PopoverContent
            side="bottom"
            align="start"
            className="p-0"
            onPointerDown={keep}
            onKeyDown={keep}
            onClick={(e) => e.stopPropagation()}
          >
            <CIList ci={ci} />
          </PopoverContent>
        </Popover>
      ) : null}
    </span>
  );
}

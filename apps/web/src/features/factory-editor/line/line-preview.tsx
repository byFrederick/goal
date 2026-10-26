// The line preview (D §10.6): the stations as a row of tiles joined by arrows, wrapping into a
// column in narrow containers; the constraint has an amber border and human stations a violet
// one; each rework rule is the hatched rust mark and a sentence. Plain HTML, a list, not a graph
// canvas. The slots sentence says how many Claude slots the line takes at full WIP.
import { ArrowDown, ArrowRight } from 'lucide-react';
import { useId } from 'react';
import { lineSlots, slotsSentence } from '../../../shared/definition/slots';
import { cn } from '../../../ui/cn';
import { Hatch } from '../../../ui/hatch';
import { previewDetail, reworkSentence, stationLabel } from './labels';
import { useLineEditor } from './line-editor';

export function LinePreview() {
  const { line, stations, constraint, slots } = useLineEditor();
  const rework = stations.map((s) => reworkSentence(s, stations)).filter((s): s is string => s !== undefined);
  const used = lineSlots(line);
  const id = useId();

  return (
    <section
      aria-labelledby={id}
      className="@container flex min-w-0 flex-col gap-3 rounded-card border border-line bg-card p-4 md:p-5"
    >
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 id={id} className="m-0 text-lg font-medium">
          Line preview
        </h2>
        {slots ? <span className="text-[13px] text-muted">{slotsSentence(used, slots.cap)}</span> : null}
      </div>
      {stations.length === 0 ? (
        <p className="m-0 text-sm text-muted">No stations yet. Add the first one above.</p>
      ) : (
        <ol aria-label="Line preview" className="m-0 flex list-none flex-col gap-0 p-0 @[640px]:flex-row">
          {stations.map((s, i) => {
            const drum = s.id === constraint;
            return (
              <li key={s.id} className="flex min-w-0 flex-col @[640px]:flex-1 @[640px]:flex-row">
                <div
                  className={cn(
                    'flex min-w-0 flex-1 flex-col gap-1 rounded-control border bg-surface p-2.5 text-[13px]',
                    drum ? 'border-amber shadow-[inset_0_3px_0_var(--color-amber)]' : 'border-line',
                    s.kind === 'human' && !drum && 'border-you-line',
                  )}
                >
                  <span className="truncate text-sm font-medium">{stationLabel(s)}</span>
                  <span className="truncate text-muted">
                    {previewDetail(s, line)}
                    {drum ? <span className="sr-only">, the constraint</span> : null}
                  </span>
                </div>
                {i < stations.length - 1 ? (
                  <span
                    aria-hidden="true"
                    className="flex h-5 items-center justify-center text-muted @[640px]:h-auto @[640px]:w-4"
                  >
                    <ArrowDown size={14} strokeWidth={1.5} className="@[640px]:hidden" />
                    <ArrowRight size={14} strokeWidth={1.5} className="hidden @[640px]:block" />
                  </span>
                ) : null}
              </li>
            );
          })}
        </ol>
      )}
      {rework.length > 0 ? (
        <ul className="m-0 flex list-none flex-col gap-1 p-0">
          {rework.map((r) => (
            <li key={r} className="flex items-center gap-2 text-[13px] text-muted">
              <span className="block h-2 w-6 shrink-0 overflow-hidden rounded-[2px]">
                <Hatch tone="rework" />
              </span>
              Rework: {r}
            </li>
          ))}
        </ul>
      ) : null}
    </section>
  );
}

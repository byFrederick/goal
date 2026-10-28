// The "Results" table (MASTER.md "Chips" and "Tables"): one row per arm with its dot, the Recommended and Now
// chips, and the report's figures. The Judged column is there only when an arm has a judged share, and the
// table scrolls in its own box, so the page never scrolls sideways.
import type { BakeOff, BakeOffArm } from '../../api/types';
import { formatDuration, formatPercent, formatUsd } from '../../shared/format';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { armDot, judgedHeading, judgedOf } from './bake-off-format';

const cellClass = 'px-3 py-3 text-right tabular-nums first:text-left';
const headClass = 'px-3 pb-3 text-right text-meta font-normal text-muted first:text-left';

/** The judge's title that names the Judged column, from the first arm that has a judged share. */
function judgeTitle(arms: readonly BakeOffArm[]): { title: string } | undefined {
  for (const arm of arms) {
    const judged = arm.summary?.judged;
    if (judged) return { title: judged.title ?? judged.judge };
  }
  return undefined;
}

export function ResultsTable({ bakeOff }: { bakeOff: BakeOff }) {
  const judge = judgeTitle(bakeOff.arms);
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-max border-collapse text-secondary">
        <caption className="sr-only">Results of the bake-off, one row per model</caption>
        <thead>
          <tr className="border-b border-line">
            <th scope="col" className={headClass}>
              Model
            </th>
            <th scope="col" className={headClass}>
              Pass rate
            </th>
            <th scope="col" className={headClass}>
              Cost per item
            </th>
            <th scope="col" className={headClass}>
              Time per item
            </th>
            <th scope="col" className={headClass}>
              Turns
            </th>
            {judge ? (
              <th scope="col" className={headClass}>
                {judgedHeading(judge.title)}
              </th>
            ) : null}
          </tr>
        </thead>
        <tbody>
          {bakeOff.arms.map((arm) => {
            const s = arm.summary;
            const recommended = bakeOff.recommended_arm === arm.arm;
            return (
              <tr key={arm.arm} className="border-b border-line last:border-b-0">
                <th scope="row" className="px-3 py-3 text-left font-normal">
                  <span className="inline-flex flex-wrap items-center gap-2">
                    <span className="inline-flex items-center gap-2 font-control text-ink">
                      <span aria-hidden="true" className={cn('size-2.5 rounded-full', armDot(arm.arm))} />
                      {arm.model}
                    </span>
                    {recommended ? <Chip tone="ok">Recommended</Chip> : null}
                    {arm.is_current ? <Chip>Now</Chip> : null}
                  </span>
                </th>
                <td className={cellClass}>{s ? formatPercent(s.pass_rate) : '–'}</td>
                <td className={cellClass}>{s ? formatUsd(s.cost_per_item_usd) : '–'}</td>
                <td className={cellClass}>{s ? formatDuration(s.time_per_item_seconds * 1000) : '–'}</td>
                <td className={cellClass}>{s ? Math.round(s.turns) : '–'}</td>
                {judge ? <td className={cellClass}>{s?.judged ? judgedOf(s.judged.share) : '–'}</td> : null}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

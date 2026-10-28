// "Tries passed per case" (MASTER.md "Chips" and the card pattern): one row per eval case, one cell per arm,
// each cell the number of tries that passed out of the tries run. The shade says how many passed and the
// number says it again, so colour is never the only signal.
import type { BakeOff } from '../../api/types';
import { cn } from '../../ui/cn';
import { armDot, cellShade, modelFamily } from './bake-off-format';

/** The grid over a finished bake-off's cases, with its arms as columns in arm order. */
export function CaseGrid({ bakeOff }: { bakeOff: BakeOff }) {
  return (
    <table className="w-full border-separate border-spacing-1 text-secondary">
      <caption className="sr-only">Tries passed per case and model, out of {bakeOff.trials}</caption>
      <thead>
        <tr>
          <th scope="col" className="px-1 pb-1 text-left text-meta font-normal text-muted">
            Case
          </th>
          {bakeOff.arms.map((arm) => (
            <th key={arm.arm} scope="col" className="px-1 pb-1 text-center text-meta font-normal text-muted">
              <span className="inline-flex items-center gap-2">
                <span aria-hidden="true" className={cn('size-2 rounded-full', armDot(arm.arm))} />
                {modelFamily(arm.model)}
              </span>
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {bakeOff.per_case.map((row) => (
          <tr key={row.case}>
            <th scope="row" className="px-1 py-0 text-left font-mono text-meta font-normal break-words text-ink">
              {row.case}
            </th>
            {bakeOff.arms.map((arm) => {
              const passed = row.arms.find((a) => a.arm === arm.arm)?.passed;
              return (
                <td key={arm.arm} className="p-0">
                  {passed === undefined ? (
                    <span className="block h-8.5 rounded-chip text-center leading-8.5 text-muted">–</span>
                  ) : (
                    <span
                      className={cn(
                        'block h-8.5 rounded-chip text-center leading-8.5 font-control tabular-nums',
                        cellShade(passed, bakeOff.trials),
                      )}
                    >
                      {passed}
                    </span>
                  )}
                </td>
              );
            })}
          </tr>
        ))}
      </tbody>
    </table>
  );
}

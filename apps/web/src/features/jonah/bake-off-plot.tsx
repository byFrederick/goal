// The "Pass rate against cost" plot (MASTER.md "Charts"): one dot per model, placed by its cost per item
// (across) and its pass rate (up). It is drawn in HTML, not SVG, so its labels keep their size on a phone
// instead of shrinking with the viewBox. The plot's role is img and its label names every point, so a
// screen reader gets the same figures as a sighted reader.
import type { BakeOffArm } from '../../api/types';
import { formatPercent, formatUsd } from '../../shared/format';
import { cn } from '../../ui/cn';
import { armDot, costTick, passRateOf, shortModel } from './bake-off-format';

interface Point {
  arm: BakeOffArm;
  pass: number;
  cost: number;
}

interface Scale {
  /** The pass rate at the bottom edge, in tenths or fifths, and the top edge (100%). */
  yMin: number;
  yTicks: number[];
  /** The cost at the right edge: at least $3, the whole dollar above the highest cost. */
  xMax: number;
  xTicks: number[];
}

/** The axes of the plot: rates from a step under the lowest pass rate to 100%, costs from $0 to the dollar above the highest. */
export function scaleOf(points: readonly Point[]): Scale {
  const minPass = Math.min(1, ...points.map((p) => p.pass));
  const maxCost = Math.max(0, ...points.map((p) => p.cost));
  const step = minPass < 0.6 ? 0.2 : 0.1;
  const yMin = Math.max(0, Math.round((Math.floor(minPass / step) * step - step) * 100) / 100);
  const yTicks: number[] = [];
  for (let v = yMin; v <= 1 + 1e-9; v += step) yTicks.push(Math.round(v * 100) / 100);

  const xMax = Math.max(3, Math.ceil(maxCost));
  const xStep = Math.ceil(xMax / 4);
  const xTicks: number[] = [];
  for (let v = 0; v <= xMax; v += xStep) xTicks.push(v);
  return { yMin, yTicks, xMax, xTicks };
}

/** A point's place in the plot area, as percentages of its width and height. */
function placeOf(point: Point, scale: Scale): { left: number; top: number } {
  const left = (point.cost / scale.xMax) * 100;
  const top = (1 - (point.pass - scale.yMin) / (1 - scale.yMin)) * 100;
  return { left, top };
}

export function PassCostPlot({ arms }: { arms: readonly BakeOffArm[] }) {
  const points: Point[] = [];
  for (const arm of arms) {
    const pass = passRateOf(arm);
    if (arm.summary && pass !== undefined) points.push({ arm, pass, cost: arm.summary.cost_per_item_usd });
  }
  const scale = scaleOf(points);
  const names = points
    .map((p) => `${shortModel(p.arm.model)} ${formatPercent(p.pass)} at ${formatUsd(p.cost)} an item`)
    .join('; ');
  const label = points.length > 0 ? `Scatter: ${names}` : 'Scatter: no model has finished its tries yet';

  return (
    <div role="img" aria-label={label} className="relative h-72 w-full">
      <span aria-hidden="true" className="absolute top-0 left-0 text-meta text-muted">
        Pass rate
      </span>

      <div aria-hidden="true" className="absolute top-6 right-4 bottom-14 left-12">
        <span className="absolute inset-y-0 left-0 border-l border-rule" />
        <span className="absolute inset-x-0 bottom-0 border-b border-rule" />
        {scale.yTicks.map((v) => (
          <span
            key={v}
            className="absolute inset-x-0 border-t border-dashed border-line"
            style={{ top: `${(1 - (v - scale.yMin) / (1 - scale.yMin)) * 100}%` }}
          >
            <span className="absolute right-full mr-2 -translate-y-1/2 text-meta text-muted tabular-nums">
              {formatPercent(v)}
            </span>
          </span>
        ))}
        {scale.xTicks.map((v) => (
          <span
            key={v}
            className="absolute top-full mt-2 -translate-x-1/2 text-meta text-muted tabular-nums"
            style={{ left: `${(v / scale.xMax) * 100}%` }}
          >
            {costTick(v)}
          </span>
        ))}
        {points.map((p) => {
          const { left, top } = placeOf(p, scale);
          const onRight = left > 70;
          return (
            <span
              key={p.arm.arm}
              className="absolute size-5 -translate-x-1/2 -translate-y-1/2"
              style={{ left: `${left}%`, top: `${top}%` }}
            >
              <span className={cn('block size-full rounded-full', armDot(p.arm.arm))} />
              <span
                className={cn(
                  'absolute top-1/2 -translate-y-1/2 whitespace-nowrap',
                  onRight ? 'right-full mr-2 text-right' : 'left-full ml-2',
                )}
              >
                <span className="block font-control text-secondary text-ink">{shortModel(p.arm.model)}</span>
                <span className="block text-meta text-ink-soft tabular-nums">
                  {formatPercent(p.pass)}, {formatUsd(p.cost)}
                </span>
              </span>
            </span>
          );
        })}
      </div>

      <span aria-hidden="true" className="absolute right-4 bottom-0 left-12 text-center text-meta text-ink-soft">
        Cost per item
      </span>
    </div>
  );
}

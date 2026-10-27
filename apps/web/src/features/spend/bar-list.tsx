// A ranked list of spend, one row a name, a bar against the largest row and the amount: By agent and
// By model. A list goald did not split (not complete) says so in its empty copy, never $0.
import { meterOf } from './meter';
import { money, percentOf } from './view';

export interface SpendRow {
  key: string;
  label: string;
  usd: number;
  /** Shown under the label in muted text, when a label alone is not unique. */
  detail?: string | undefined;
}

export function BarList({
  title,
  rows,
  empty,
  labelledBy,
}: {
  title: string;
  rows: readonly SpendRow[];
  /** What the card says when there are no rows. */
  empty: string;
  labelledBy: string;
}) {
  const top = Math.max(0, ...rows.map((r) => r.usd));
  return (
    <section aria-labelledby={labelledBy} className="flex min-w-0 flex-col rounded-card border border-line bg-card p-4">
      <h2 id={labelledBy} className="m-0 mb-4 text-heading text-ink">
        {title}
      </h2>
      {rows.length === 0 ? (
        <p className="m-0 text-secondary text-muted">{empty}</p>
      ) : (
        <ul className="m-0 flex list-none flex-col gap-2 p-0">
          {rows.map((r) => (
            <li key={r.key} className="flex items-center gap-3 text-secondary">
              <span className="w-36 min-w-0 shrink-0 truncate text-ink-soft">
                {r.label}
                {r.detail ? <span className="block text-meta text-muted">{r.detail}</span> : null}
              </span>
              <span
                {...meterOf(`${r.label}: ${money(r.usd)}`, r.usd, top)}
                className="relative h-3 min-w-0 flex-1 overflow-hidden rounded-chip bg-panel"
              >
                <span className="block h-full rounded-chip bg-run" style={{ width: `${percentOf(r.usd, top)}%` }} />
              </span>
              <span className="w-16 shrink-0 text-right font-numeral font-semibold text-ink">{money(r.usd)}</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

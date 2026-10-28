// Spend by day, one stacked bar a UTC day, each segment a factory in its legend colour. A day goald
// did not split by factory is drawn as one neutral segment and the note says why. The bars are
// decorative: a visually hidden table carries every value for screen readers.
import type { OrgSpend } from '../../api/types';
import { factoryLines, money, shortDay, weekdayOf } from './view';

export function SpendDays({ spend }: { spend: OrgSpend }) {
  const lines = factoryLines(spend);
  const top = Math.max(0, ...spend.days.map((d) => d.usd));
  const fillOf = new Map(lines.map((l) => [l.id, l.fill]));
  const nameOf = new Map(lines.map((l) => [l.id, l.name]));

  return (
    <section aria-labelledby="day-h" className="flex min-w-0 flex-col rounded-card border border-line bg-card p-4">
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <h2 id="day-h" className="m-0 min-w-0 flex-1 text-heading text-ink">
          Spend by day
        </h2>
        <ul className="m-0 flex list-none flex-wrap gap-x-4 gap-y-2 p-0 text-meta text-ink-soft">
          {lines.map((l) => (
            <li key={l.id} className="flex items-center gap-2">
              <i aria-hidden="true" className={`block size-3 rounded-chip ${l.fill}`} />
              {l.name} {money(l.usd)}
            </li>
          ))}
        </ul>
      </div>
      <div className="flex h-56 gap-3">
        <div aria-hidden="true" className="flex flex-col justify-between pb-6 text-right text-meta text-muted">
          <span>{money(top)}</span>
          <span>{money(top / 2)}</span>
          <span>$0</span>
        </div>
        <div aria-hidden="true" className="relative flex min-w-0 flex-1 items-end gap-1 border-b border-line pb-6">
          <span className="pointer-events-none absolute inset-x-0 top-0 border-t border-dashed border-line" />
          <span className="pointer-events-none absolute inset-x-0 top-1/2 border-t border-dashed border-line" />
          {spend.days.map((d, i) => (
            <div key={d.day} className="relative flex h-full min-w-0 flex-1 flex-col-reverse">
              {d.usd > 0 ? (
                <div
                  className="flex flex-col-reverse gap-1 overflow-hidden rounded-chip"
                  style={{ height: `${top > 0 ? (d.usd / top) * 100 : 0}%` }}
                >
                  {spend.complete ? (
                    d.by_factory.map((f) => (
                      <span
                        key={f.factory_id}
                        className={`block ${fillOf.get(f.factory_id) ?? 'bg-run'}`}
                        style={{ height: `${d.usd > 0 ? (f.usd / d.usd) * 100 : 0}%` }}
                      />
                    ))
                  ) : (
                    <span className="block h-full bg-hover" />
                  )}
                </div>
              ) : null}
              {labelOf(spend.window, i, d.day, spend.days.length) ? (
                <b className="absolute -bottom-6 inset-x-0 text-center text-meta font-normal text-muted">
                  {labelOf(spend.window, i, d.day, spend.days.length)}
                </b>
              ) : null}
            </div>
          ))}
        </div>
      </div>
      {spend.complete ? null : (
        <p className="m-0 mt-3 text-meta text-muted">
          Some spend is not split by factory: a factory you cannot see spent it.
        </p>
      )}
      <table className="sr-only">
        <caption>Spend by day, {spend.days.length} days</caption>
        <thead>
          <tr>
            <th scope="col">Day</th>
            <th scope="col">Total</th>
            <th scope="col">By factory</th>
          </tr>
        </thead>
        <tbody>
          {spend.days.map((d) => (
            <tr key={d.day}>
              <th scope="row">{shortDay(d.day)}</th>
              <td>{money(d.usd)}</td>
              <td>
                {spend.complete
                  ? d.by_factory.map((f) => `${nameOf.get(f.factory_id) ?? 'Factory'} ${money(f.usd)}`).join(', ') ||
                    '—'
                  : '—'}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}

/** A bar's label: every day of a week, then every fifth day of a month and every fourteenth of 90 days. */
function labelOf(window: OrgSpend['window'], index: number, day: string, count: number): string | null {
  if (window === 'week') return weekdayOf(day);
  const step = count <= 31 ? 5 : 14;
  return index % step === 0 ? shortDay(day) : null;
}

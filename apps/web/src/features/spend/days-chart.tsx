// Spend by day, one stacked bar a UTC day of the window (days after today are empty slots), each segment a factory in its legend colour. A day goald
// did not split by factory is drawn as one neutral segment and the note says why. The bars are
// decorative: a visually hidden table carries every value for screen readers.
import type { OrgSpend } from '../../api/types';
import { factoryLines, money, shortDay, weekdayOf, windowDays } from './view';

export function SpendDays({ spend }: { spend: OrgSpend }) {
  const lines = factoryLines(spend);
  const top = Math.max(0, ...spend.days.map((d) => d.usd));
  const fillOf = new Map(lines.map((l) => [l.id, l.fill]));
  const nameOf = new Map(lines.map((l) => [l.id, l.name]));
  const byDay = new Map(spend.days.map((d) => [d.day, d]));
  const slots = windowDays(spend);
  const todayOf = spend.until.slice(0, 10);

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
          {slots.map((slot, i) => {
            const d = byDay.get(slot);
            const today = slot === todayOf;
            const label = labelOf(spend.window, i, slot, slots.length, today);
            return (
              <div
                key={slot}
                data-day={slot}
                data-today={today || undefined}
                className="relative flex h-full min-w-0 flex-1 flex-col-reverse items-center"
              >
                {d && d.usd > 0 ? (
                  <div
                    className={`flex w-full max-w-12 flex-col-reverse gap-1 overflow-hidden rounded-chip ${today ? 'opacity-45' : ''}`}
                    style={{ height: `${top > 0 ? (d.usd / top) * 100 : 0}%` }}
                  >
                    {spend.complete ? (
                      d.by_factory.map((f) => (
                        <span
                          key={f.factory_id}
                          className={`block ${fillOf.get(f.factory_id) ?? 'bg-hover'}`}
                          style={{ height: `${(f.usd / d.usd) * 100}%` }}
                        />
                      ))
                    ) : (
                      <span className="block h-full bg-hover" />
                    )}
                  </div>
                ) : null}
                {label ? (
                  <b className="absolute -bottom-6 left-1/2 -translate-x-1/2 whitespace-nowrap text-center text-meta font-normal text-muted">
                    {label}
                  </b>
                ) : null}
              </div>
            );
          })}
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
            <th scope="col" className="font-medium">
              Day
            </th>
            <th scope="col" className="font-medium">
              Total
            </th>
            <th scope="col" className="font-medium">
              By factory
            </th>
          </tr>
        </thead>
        <tbody>
          {spend.days.map((d) => (
            <tr key={d.day}>
              <th scope="row" className="font-medium">
                {shortDay(d.day)}
              </th>
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

/** A slot's label: every day of a week ("Today" for today), then every fifth day of a month and every fourteenth of 90 days. */
function labelOf(window: OrgSpend['window'], index: number, day: string, count: number, today: boolean): string | null {
  if (window === 'week') return today ? 'Today' : weekdayOf(day);
  const step = count <= 31 ? 5 : 14;
  return index % step === 0 ? shortDay(day) : null;
}

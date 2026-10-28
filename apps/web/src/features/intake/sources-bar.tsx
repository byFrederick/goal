// "Where the last N items came from" (intake, MASTER.md's categorical series): one bar of the newest items'
// sources and its legend. The series take foundation tokens, not factory colours, which are for badges.
import type { Intake } from '../../api/types';
import { formatDay, plural } from '../../shared/format';
import { cn } from '../../ui/cn';

interface Series {
  label: string;
  value: number;
  tone: string;
}

/** The sources in the order the bar and legend show them. */
function seriesOf({ sources }: Intake): Series[] {
  return [
    { label: 'GitHub', value: sources.github, tone: 'bg-run' },
    { label: 'People, with New item', value: sources.people, tone: 'bg-ok' },
    { label: 'CLI and API', value: sources.cli + sources.api, tone: 'bg-ink-soft' },
    { label: 'Schedules', value: sources.schedule, tone: 'bg-run-soft' },
    { label: 'Webhooks', value: sources.webhook, tone: 'bg-muted' },
  ];
}

export function SourcesBar({ intake }: { intake: Intake }) {
  const series = seriesOf(intake);
  const total = series.reduce((sum, s) => sum + s.value, 0);
  const summary = series.map((s) => `${s.label} ${s.value}`).join(', ');
  const span =
    intake.from && intake.to
      ? intake.from === intake.to
        ? formatDay(intake.from)
        : `${formatDay(intake.from)} to ${formatDay(intake.to)}`
      : null;

  return (
    <section aria-labelledby="sources-heading" className="flex flex-col gap-4 rounded-card border border-line p-5">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <h2 id="sources-heading" className="m-0 text-heading">
          Where the last {plural(intake.items, 'item')} came from
        </h2>
        {span ? <span className="text-secondary text-muted">{span}</span> : null}
      </div>
      <div className="flex h-3.5 overflow-hidden rounded-chip bg-hover" role="img" aria-label={summary}>
        {total > 0
          ? series.map((s) =>
              s.value > 0 ? (
                <span
                  key={s.label}
                  className={cn('block h-full', s.tone)}
                  style={{ width: `${(s.value / total) * 100}%` }}
                />
              ) : null,
            )
          : null}
      </div>
      <ul className="m-0 flex list-none flex-wrap gap-x-5 gap-y-2 p-0 text-secondary">
        {series.map((s) => (
          <li key={s.label} className="flex items-center gap-2">
            <span aria-hidden="true" className={cn('size-2.5 rounded-chip', s.tone)} />
            {s.label} <b className="font-semibold">{s.value}</b>
          </li>
        ))}
      </ul>
    </section>
  );
}

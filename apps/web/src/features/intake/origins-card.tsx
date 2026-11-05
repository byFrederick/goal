// Where items came from (Intake, ADR 0040 decision 2): how many of the factory's items created in the window
// came from a person, an assembly's plan, follow-ups or triggers, as one bar and its legend. The window is a
// segmented control; the series take foundation tokens, not factory colours (MASTER.md, categorical series).
import type { IntakeWindow } from '../../api/intake';
import type { IntakeOrigins } from '../../api/types';
import { cn } from '../../ui/cn';
import { SegmentedControl } from '../../ui/segmented-control';
import { Panel } from './panel';

const windows: { value: IntakeWindow; label: string }[] = [
  { value: '7d', label: '7 days' },
  { value: '30d', label: '30 days' },
  { value: '90d', label: '90 days' },
];

const info =
  'People counts New item, the CLI and the API. Plans are the parts of assemblies. Follow-ups are items agents asked for.';

function seriesOf(origins: IntakeOrigins) {
  return [
    { label: 'People', value: origins.person, tone: 'bg-ink' },
    { label: 'Plans', value: origins.plan, tone: 'bg-rule' },
    { label: 'Follow-ups', value: origins.follow_up, tone: 'bg-run' },
    { label: 'Triggers', value: origins.trigger, tone: 'bg-you' },
  ];
}

export function OriginsCard({
  origins,
  window,
  onWindowChange,
}: {
  origins: IntakeOrigins;
  window: IntakeWindow;
  onWindowChange: (window: IntakeWindow) => void;
}) {
  const series = seriesOf(origins);
  const total = series.reduce((sum, s) => sum + s.value, 0);
  const summary = series.map((s) => `${s.label} ${s.value}`).join(', ');
  return (
    <Panel
      title="Where items came from"
      info={info}
      aside={<SegmentedControl label="Window" options={windows} value={window} onChange={onWindowChange} />}
    >
      <div className="flex flex-col gap-3 px-5 pt-1 pb-5">
        <div className="flex h-3.5 overflow-hidden rounded-chip bg-hover" role="img" aria-label={summary}>
          {series.map((s) =>
            s.value > 0 ? (
              <span
                key={s.label}
                className={cn('block h-full origin-left motion-safe:animate-fade-in', s.tone)}
                style={{ width: `${(s.value / total) * 100}%` }}
              />
            ) : null,
          )}
        </div>
        <ul className="m-0 flex list-none flex-wrap gap-x-5 gap-y-2 p-0 text-secondary">
          {series.map((s) => (
            <li key={s.label} className="flex items-center gap-2">
              <span aria-hidden="true" className={cn('size-2.5 rounded-chip', s.tone)} />
              {s.label} <b className="font-semibold tabular-nums">{s.value}</b>
            </li>
          ))}
        </ul>
      </div>
    </Panel>
  );
}

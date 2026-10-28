// The Triggers table (intake): each trigger of the running definition with its source, when it fires, what
// it creates, its last 7 days and its switch. A problem is named in red under the name. The table is a
// table from 768 px; below it each row stacks its cells, with no column headings.
import type { LucideIcon } from 'lucide-react';
import { Clock, GitBranch, Webhook } from 'lucide-react';
import type { IntakeTrigger } from '../../api/types';
import { useFactoryParams } from '../../shared/factory';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { Switch } from '../../ui/switch';
import { useTriggerToggle } from './trigger-toggle';

/** The square and icon each source is drawn with (its tile is a foundation token, its icon white). */
const sourceIcon: Record<IntakeTrigger['source'], { icon: LucideIcon; tile: string; label: string }> = {
  github: { icon: GitBranch, tile: 'bg-ink', label: 'GitHub' },
  schedule: { icon: Clock, tile: 'bg-muted', label: 'Schedule' },
  webhook: { icon: Webhook, tile: 'bg-run', label: 'Webhook' },
};

export function TriggersTable({ triggers }: { triggers: readonly IntakeTrigger[] }) {
  return (
    <section aria-labelledby="triggers-heading" className="rounded-card border border-line">
      <div className="flex flex-wrap items-center gap-3 p-5 pb-3">
        <h2 id="triggers-heading" className="m-0 text-heading">
          Triggers
        </h2>
        <span className="rounded-chip bg-hover px-2 text-meta font-semibold text-ink-soft font-numeral">
          {triggers.length}
        </span>
        <p className="m-0 text-secondary text-muted md:ml-auto">New item, the goal CLI and the API are always on</p>
      </div>
      <div className="overflow-x-auto px-2 pb-2 md:px-3">
        <table className="block w-full border-collapse text-secondary md:table">
          <thead className="hidden md:table-header-group">
            <tr>
              <th scope="col" className="px-3 pb-3 text-left text-meta font-semibold text-muted">
                Trigger
              </th>
              <th scope="col" className="px-3 pb-3 text-left text-meta font-semibold text-muted">
                When
              </th>
              <th scope="col" className="px-3 pb-3 text-left text-meta font-semibold text-muted">
                Creates
              </th>
              <th scope="col" className="px-3 pb-3 text-left text-meta font-semibold text-muted">
                Last 7 days
              </th>
              <th scope="col" className="px-3 pb-3 text-right text-meta font-semibold text-muted">
                <span className="sr-only">On</span>
              </th>
            </tr>
          </thead>
          <tbody className="block md:table-row-group">
            {triggers.length === 0 ? (
              <tr className="block px-3 py-4 text-muted md:table-row">
                <td className="block md:table-cell md:py-4" colSpan={5}>
                  No triggers yet. Add one with New trigger.
                </td>
              </tr>
            ) : null}
            {triggers.map((trigger) => (
              <TriggerRow key={trigger.name} trigger={trigger} />
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function TriggerRow({ trigger }: { trigger: IntakeTrigger }) {
  const params = useFactoryParams();
  const toggle = useTriggerToggle(trigger.name, trigger.enabled);
  const { icon: Icon, tile } = sourceIcon[trigger.source];
  const items = trigger.days.reduce((sum, n) => sum + n, 0);

  return (
    <tr className="block border-t border-line px-3 py-3 first:border-t-0 md:table-row md:p-0">
      <td className="block py-1 md:table-cell md:px-3 md:py-3 md:align-middle">
        <div className="flex min-w-0 items-start gap-3">
          <span
            aria-hidden="true"
            className={cn('mt-px flex size-8 flex-none items-center justify-center rounded-cell text-white', tile)}
          >
            <Icon className="size-4" />
          </span>
          <div className="flex min-w-0 flex-col gap-px">
            <a
              href={`/o/${params.org}/f/${params.factory}/intake/${encodeURIComponent(trigger.name)}`}
              className="font-semibold text-ink no-underline hover:text-run-ink"
            >
              {trigger.name}
            </a>
            {trigger.problem ? (
              <span className="text-meta font-semibold text-rework-ink">{trigger.problem.message}</span>
            ) : null}
            {toggle.notice ? (
              <span role="alert" className="text-meta text-rework-ink">
                {toggle.notice}
              </span>
            ) : null}
          </div>
        </div>
      </td>
      <td className="block py-1 text-ink-soft md:table-cell md:px-3 md:py-3 md:align-middle">{trigger.when}</td>
      <td className="block py-1 md:table-cell md:px-3 md:py-3 md:align-middle">
        <div className="flex flex-wrap items-center gap-2">
          <Chip>{trigger.create.type}</Chip>
          <span className="text-ink-soft">at {trigger.create.station}</span>
          {trigger.create.expedite ? <Chip tone="expedite">expedite</Chip> : null}
        </div>
      </td>
      <td className="block py-1 md:table-cell md:px-3 md:py-3 md:align-middle">
        <LastWeek days={trigger.days} items={items} />
      </td>
      <td className="block pt-2 md:table-cell md:px-3 md:py-3 md:text-right md:align-middle">
        <Switch
          aria-label={trigger.name}
          checked={toggle.checked}
          disabled={toggle.disabled}
          onCheckedChange={toggle.toggle}
        />
      </td>
    </tr>
  );
}

/** The seven days' items as bars, the last (today) in cobalt; the number beside them is the total. */
function LastWeek({ days, items }: { days: readonly number[]; items: number }) {
  const top = Math.max(1, ...days);
  return (
    <div className="flex items-center gap-3">
      <span aria-hidden="true" className="flex h-4.5 w-10 items-end gap-px">
        {days.map((n, i) => (
          <span
            // The seven days are a fixed window: their position is their identity.
            // biome-ignore lint/suspicious/noArrayIndexKey: a day's position is its identity
            key={i}
            className={cn('w-1.5', i === days.length - 1 ? 'bg-run' : 'bg-run-soft')}
            style={{ height: `${(n / top) * 100}%` }}
          />
        ))}
      </span>
      <span className="font-numeral font-semibold text-ink">
        {items}
        <span className="sr-only"> items in the last 7 days</span>
      </span>
    </div>
  );
}

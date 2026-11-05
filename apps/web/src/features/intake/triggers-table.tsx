// The Triggers table (intake): each trigger of the running definition with its source, when it fires, what
// it creates, its last 7 days and its switch. A problem is named in red under the name. The table is a
// table from 640 px of room; below it each row stacks its cells, with no column headings.
import { Link } from '@tanstack/react-router';
import type { LucideIcon } from 'lucide-react';
import { Clock, GitBranch, Webhook } from 'lucide-react';
import type { IntakeTrigger } from '../../api/types';
import { useFactoryParams } from '../../shared/factory';
import { useArrivals } from '../../ui/arrivals';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { InfoTip } from '../../ui/info-tip';
import { Switch } from '../../ui/switch';
import { useTriggerToggle } from './trigger-toggle';

/** The square and icon each source is drawn with (its tile is the near-black mark unless blue, its icon white). */
export const sourceIcon: Record<IntakeTrigger['source'], { icon: LucideIcon; tile?: string; label: string }> = {
  github: { icon: GitBranch, label: 'GitHub' },
  schedule: { icon: Clock, label: 'Schedule' },
  webhook: { icon: Webhook, tile: 'bg-run', label: 'Webhook' },
};

export function TriggersTable({ triggers }: { triggers: readonly IntakeTrigger[] }) {
  const arrived = useArrivals(triggers.map((trigger) => trigger.name));
  return (
    <section aria-labelledby="triggers-heading" className="min-w-0 rounded-card border border-line bg-card">
      <div className="flex min-h-12 items-center gap-1 px-5 pt-3 pb-2">
        <h2 id="triggers-heading" className="m-0 text-heading">
          Triggers
        </h2>
        <InfoTip about="Triggers">
          Triggers live in the factory's definition. Off ones are suggestions; turning one on publishes a new version.
          New item, the goal CLI and the API are always on.
        </InfoTip>
        <span className="ml-auto text-secondary text-muted tabular-nums">
          {triggers.filter((t) => t.enabled).length} on
        </span>
      </div>
      <div className="relative overflow-x-auto px-2 pb-2 @[640px]:px-3">
        <table className="block w-full border-collapse text-secondary @[640px]:table">
          <thead className="hidden @[640px]:table-header-group">
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
          <tbody className="block @[640px]:table-row-group">
            {triggers.length === 0 ? (
              <tr className="block px-3 py-4 text-muted @[640px]:table-row">
                <td className="block @[640px]:table-cell @[640px]:py-4" colSpan={5}>
                  No triggers yet. Add one with New trigger.
                </td>
              </tr>
            ) : null}
            {triggers.map((trigger) => (
              <TriggerRow key={trigger.name} trigger={trigger} arrived={arrived(trigger.name)} />
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function TriggerRow({ trigger, arrived }: { trigger: IntakeTrigger; arrived: boolean }) {
  const params = useFactoryParams();
  const toggle = useTriggerToggle(trigger.name, trigger.enabled);
  const { icon: Icon } = sourceIcon[trigger.source];
  const items = trigger.days.reduce((sum, n) => sum + n, 0);

  return (
    <tr
      className={cn(
        'block border-t border-line px-3 py-3 first:border-t-0 @[640px]:table-row @[640px]:p-0',
        arrived && 'motion-safe:animate-item-in',
      )}
    >
      <td className="block py-1 @[640px]:table-cell @[640px]:px-3 @[640px]:py-3 @[640px]:align-middle">
        <div className="flex min-w-0 items-start gap-3">
          <span aria-hidden="true" className="mt-px flex size-4 flex-none items-center justify-center text-muted">
            <Icon className="size-4" />
          </span>
          <div className="flex min-w-0 flex-col gap-px">
            <a
              href={`/o/${params.org}/f/${params.factory}/intake/${encodeURIComponent(trigger.name)}`}
              className="font-semibold text-ink no-underline hover:text-run-ink"
            >
              {trigger.name}
            </a>
            {trigger.problem?.kind === 'missing_connection' ? (
              <span className="text-meta text-muted">
                {needsText(trigger)}{' '}
                <Link
                  to="/o/$org/f/$factory/intake/webhooks"
                  params={{ org: params.org, factory: params.factory }}
                  className="font-medium text-ink underline-offset-2 hover:underline"
                >
                  Set up
                </Link>
              </span>
            ) : trigger.problem ? (
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
      <td className="block py-1 text-ink-soft @[640px]:table-cell @[640px]:px-3 @[640px]:py-3 @[640px]:align-middle">
        {trigger.when}
      </td>
      <td className="block py-1 @[640px]:table-cell @[640px]:px-3 @[640px]:py-3 @[640px]:align-middle">
        <div className="flex flex-wrap items-center gap-2">
          {trigger.check ? (
            <>
              <Chip tone="plain">Check</Chip>
              <span className="text-ink-soft">by {trigger.check.agent}</span>
            </>
          ) : (
            <>
              <Chip>{trigger.create.type}</Chip>
              <span className="text-ink-soft">at {trigger.create.station}</span>
            </>
          )}
          {trigger.create.expedite ? <Chip tone="expedite">expedite</Chip> : null}
        </div>
      </td>
      <td className="hidden @[640px]:table-cell @[640px]:px-3 @[640px]:py-3 @[640px]:align-middle">
        {trigger.enabled ? <LastWeek days={trigger.days} items={items} /> : <span className="text-muted">Off</span>}
      </td>
      <td className="block pt-2 @[640px]:table-cell @[640px]:px-3 @[640px]:py-3 @[640px]:text-right @[640px]:align-middle">
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

/** What a trigger waits for: its webhook endpoint, or access to its source. */
function needsText(trigger: IntakeTrigger): string {
  if (trigger.source === 'webhook')
    return `Needs a ${trigger.on.replace(/^webhook\./, '').split('.')[0] ?? 'webhook'} webhook.`;
  return `Needs ${trigger.source === 'github' ? 'GitHub' : sourceIcon[trigger.source].label} access.`;
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
            className={cn('w-1.5', i === days.length - 1 ? 'bg-ink' : 'bg-rule')}
            style={{ height: `${(n / top) * 100}%` }}
          />
        ))}
      </span>
      <span className="font-semibold text-ink">
        {items}
        <span className="sr-only"> items in the last 7 days</span>
      </span>
    </div>
  );
}

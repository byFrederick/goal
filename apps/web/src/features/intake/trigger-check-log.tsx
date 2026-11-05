// A check trigger's log (ADR 0040 decision 19, the Recent checks card of intake/trigger-check.jpg): each check
// with when it ran, how it ended, what it filed, noted and skipped with the reason, and what it cost. A try
// is marked as a try and says what it would have done. Every key, title, text and reason comes from an agent
// or from the source it read, so all of it is shown as text.
import { Link } from '@tanstack/react-router';
import type { ReactNode } from 'react';
import type { TriggerCheck } from '../../api/types';
import { Chip, type ChipTone } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { EmptyState } from '../../ui/empty-state';
import { NumberRoll } from '../../ui/number-roll';
import { useArrivals } from '../../ui/arrivals';
import { InfoTip } from '../jonah/info-tip';
import { eventWhen } from './trigger-events';

const cost = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 3 });
const dollars = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' });

/** The number of a ref: "#91" is 91, the address an item opens at. */
const numberOf = (ref: string) => ref.replace(/^.*#/, '');

/** How a check ended, as the chip says it: a try says what it would have done. */
export function statusOf(check: TriggerCheck): { tone: ChipTone; text: string } {
  if (check.state === 'queued') return { tone: 'plain', text: 'Queued' };
  if (check.state === 'running') return { tone: 'run', text: 'Checking' };
  if (check.state === 'failed') return { tone: 'rework', text: 'Failed' };
  const refs = (list: { ref: string | null }[]) => list.flatMap((e) => (e.ref ? [e.ref] : []));
  const filed = refs(check.filed);
  const noted = refs(check.noted);
  if (check.try) {
    if (check.filed.length) return { tone: 'plain', text: `Would file ${check.filed.length}` };
    if (check.noted.length) return { tone: 'plain', text: `Would note ${check.noted.length}` };
    return { tone: 'plain', text: 'Nothing new' };
  }
  if (filed.length) return { tone: 'run', text: `Filed ${filed.join(', ')}` };
  if (noted.length) return { tone: 'plain', text: `Noted ${noted.join(', ')}` };
  return { tone: 'plain', text: 'Nothing new' };
}

/** "Today: 168 checks, 2 items, $0.41": the day's checks that were not tries, with their numbers rolling in place. */
export function TodaySummary({ checks, now = new Date(), className }: { checks: TriggerCheck[]; now?: Date; className?: string }) {
  const today = checks.filter((c) => !c.try && new Date(c.at).toDateString() === now.toDateString());
  const items = today.reduce((n, c) => n + c.filed.length, 0);
  const spent = today.reduce((n, c) => n + c.cost_usd, 0);
  return (
    <p className={className}>
      Today: <NumberRoll value={today.length} /> {today.length === 1 ? 'check' : 'checks'}, <NumberRoll value={items} />{' '}
      {items === 1 ? 'item' : 'items'}, <NumberRoll value={dollars.format(spent)} />
    </p>
  );
}

function ItemLink({ item, org, factory }: { item: string; org: string; factory: string }) {
  return (
    <Link
      to="/o/$org/f/$factory/items/$ref"
      params={{ org, factory, ref: numberOf(item) }}
      className="font-semibold text-link underline"
    >
      {item}
    </Link>
  );
}

function Line({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[4.5rem_minmax(0,1fr)] gap-x-3 text-secondary">
      <dt className="text-muted">{label}</dt>
      <dd className="m-0 min-w-0 break-words">{children}</dd>
    </div>
  );
}

function Row({ check, org, factory, arrived, now }: { check: TriggerCheck; org: string; factory: string; arrived: boolean; now?: Date | undefined }) {
  const status = statusOf(check);
  return (
    <li
      className={cn(
        'grid grid-cols-[4.5rem_minmax(0,1fr)_auto] items-start gap-x-3 border-t border-line py-3 first:border-t-0',
        arrived && 'motion-safe:animate-row-in motion-reduce:animate-fade-in',
      )}
    >
      <span className="text-secondary text-muted">{eventWhen(check.at, now)}</span>
      <div className="flex min-w-0 flex-col gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <Chip tone={status.tone}>{status.text}</Chip>
          {check.try ? <Chip tone="quiet">Try</Chip> : null}
        </div>
        {check.state === 'failed' && check.reason ? (
          <dl className="m-0 flex flex-col gap-1">
            <Line label="Why">{check.reason}</Line>
          </dl>
        ) : null}
        {check.filed.length || check.noted.length || check.skipped.length ? (
          <dl className="m-0 flex flex-col gap-1">
            {check.filed.map((f) => (
              <Line key={`f-${f.key}`} label={check.try ? 'Would file' : 'Filed'}>
                {f.key} {f.title}
                {f.ref ? (
                  <>
                    {' '}
                    <ItemLink item={f.ref} org={org} factory={factory} />
                  </>
                ) : null}
                {f.reopens ? <span className="text-muted">{`, reopens ${f.reopens}`}</span> : null}
              </Line>
            ))}
            {check.noted.map((n) => (
              <Line key={`n-${n.key}-${n.ref}`} label={check.try ? 'Would note' : 'Noted'}>
                {n.key} on <ItemLink item={n.ref} org={org} factory={factory} />: {n.text}
              </Line>
            ))}
            {check.skipped.map((s) => (
              <Line key={`s-${s.key}`} label="Skipped">
                {s.key} {s.reason}
              </Line>
            ))}
          </dl>
        ) : null}
      </div>
      <span className="text-secondary text-ink-soft">{cost.format(check.cost_usd)}</span>
    </li>
  );
}

export function RecentChecks({
  checks,
  org,
  factory,
  every,
  now,
}: {
  checks: TriggerCheck[];
  /** The organization's slug and the factory's name, which the item links go to. */
  org: string;
  factory: string;
  /** "Every 5 minutes": when the trigger fires. */
  every?: string | undefined;
  now?: Date | undefined;
}) {
  const arrived = useArrivals(checks.map((c) => c.id));
  return (
    <section aria-labelledby="recent-checks" className="flex flex-col rounded-card border border-line p-4">
      <div className="flex items-center gap-1">
        <h2 id="recent-checks" className="m-0 text-heading">
          Recent checks
        </h2>
        <InfoTip label="About recent checks">
          Each check, with what the agent filed, noted and skipped, and why. Try runs one now without filing anything.
        </InfoTip>
        {every ? <span className="ml-auto text-secondary text-muted">{every}</span> : null}
      </div>
      {checks.length === 0 ? (
        <EmptyState title="No checks yet">The trigger lists each check here, with what it did.</EmptyState>
      ) : (
        <ol className="m-0 mt-2 flex list-none flex-col p-0">
          {checks.map((check) => (
            <Row key={check.id} check={check} org={org} factory={factory} arrived={arrived(check.id)} now={now} />
          ))}
        </ol>
      )}
    </section>
  );
}

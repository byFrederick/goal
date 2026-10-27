// What a trigger did: this week's totals (the line under the title) and its recent events, newest first,
// each with an outcome chip. The event's text is the value of its dedupe field, or its event key.
import type { IntakeEvent, IntakeTriggerDetail } from '../../api/types';
import { Chip, type ChipTone } from '../../ui/chip';
import { EmptyState } from '../../ui/empty-state';

const money = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' });

/** "Created 3 items this week, 2 merged, $7.80 in all" (the picture's line under the title). */
export function weekLine(week: IntakeTriggerDetail['week']): string {
  if (week.created === 0) return 'No items created this week';
  const items = week.created === 1 ? '1 item' : `${week.created} items`;
  return `Created ${items} this week, ${week.merged} merged, ${money.format(week.spend_usd)} in all`;
}

/** "Today 14:05", "Yesterday 18:20" or "Oct 23": the day of an event, then its time on the same day. */
export function eventWhen(at: string, now: Date = new Date()): string {
  const when = new Date(at);
  const yesterday = new Date(now);
  yesterday.setDate(now.getDate() - 1);
  const time = when.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' });
  if (when.toDateString() === now.toDateString()) return `Today ${time}`;
  if (when.toDateString() === yesterday.toDateString()) return `Yesterday ${time}`;
  return when.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}

/** The outcome chip: created (cobalt), merged (green) or skipped with its reason (plain). */
function outcome(event: IntakeEvent): { tone: ChipTone; text: string } {
  const number = event.item_number === null ? '' : `#${event.item_number}`;
  if (event.outcome === 'skipped') {
    return { tone: 'plain', text: event.reason ? `Skipped, ${event.reason}` : 'Skipped' };
  }
  if (event.outcome === 'merged') return { tone: 'ok', text: number ? `${number} merged` : 'Merged' };
  return { tone: 'run', text: number ? `Created ${number}` : 'Created' };
}

export function RecentEvents({ events, now }: { events: IntakeEvent[]; now?: Date }) {
  return (
    <section aria-labelledby="recent-events" className="flex flex-col rounded-card border border-line p-4">
      <h2 id="recent-events" className="m-0 mb-1 text-heading">
        Recent events
      </h2>
      {events.length === 0 ? (
        <EmptyState title="No events yet">
          The trigger lists each event it receives here, with what it did about it.
        </EmptyState>
      ) : (
        <ol className="m-0 flex list-none flex-col p-0">
          {events.map((event) => {
            const chip = outcome(event);
            return (
              <li
                key={`${event.at}-${event.title}-${event.item_number ?? ''}`}
                className="flex items-start justify-between gap-3 border-t border-line py-2.5 first:border-t-0"
              >
                <div className="flex min-w-0 flex-col gap-0.5">
                  <span className="min-w-0 break-words text-body">{event.title}</span>
                  <span className="text-meta text-muted">{eventWhen(event.at, now)}</span>
                </div>
                <Chip tone={chip.tone}>{chip.text}</Chip>
              </li>
            );
          })}
        </ol>
      )}
    </section>
  );
}

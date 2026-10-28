// The last seven days of an endpoint's deliveries as bars, oldest first. A day with a refused delivery
// is red; the bar's height is the day's share of the busiest day. The words beside the bars are the
// screen's text, so the bars are decoration for people who see them (aria-hidden).
import type { WebhookDeliveryDay } from '../../api/webhooks';
import { weekSummary } from './words';

export function DayBars({ days }: { days: readonly WebhookDeliveryDay[] }) {
  const busiest = Math.max(1, ...days.map((d) => d.delivered + d.refused));
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-3">
      <span aria-hidden="true" className="flex h-5.5 items-end gap-1">
        {days.map((day) => {
          const total = day.delivered + day.refused;
          // A day with no deliveries is a flat stub, so the week still reads as seven days.
          const height = total === 0 ? 8 : Math.max(16, Math.round((total / busiest) * 100));
          return (
            <i
              key={day.day}
              title={`${day.day}: ${weekSummary([day])}`}
              style={{ height: `${height}%` }}
              className={`block w-2 rounded-chip ${day.refused > 0 ? 'bg-rework' : 'bg-run-soft'}`}
            />
          );
        })}
      </span>
      <span className="text-secondary text-ink">{weekSummary(days)}</span>
    </div>
  );
}

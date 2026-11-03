// The table of the account page's Notifications section (docs/design/screens/account/account.html): which
// events the person is told about, on which channel (ADR 0023). With sign-in the saved preferences come from
// GET /v1/me/notifications, and every box PUTs them whole. The Slack column is live only when the
// organization's notification setting names a Slack secret (ADR 0037); with none its boxes show the saved
// value and are NotAvailable. With
// sign-in off there is no person, so nothing is requested and every box is NotAvailable.
import { useQuery } from '@tanstack/react-query';
import { toast } from 'sonner';
import { notificationsQuery, orgNotificationsQuery } from '../../api/queries';
import type { NotificationChannels, NotificationEvents, NotificationPreferences } from '../../api/types';
import { usePhone } from '../../app/phone-chrome';
import { Button } from '../../ui/button';
import { Checkbox } from '../../ui/checkbox';
import { NotAvailable } from '../../ui/not-available';
import { Skeleton } from '../../ui/skeleton';
import { type PreferencesChange, useSaveNotifications } from './notification-preferences';

type Event = keyof NotificationEvents;
type Channel = keyof NotificationChannels;

/** The rows of the picture, in its order. The digest has no In Goal channel. */
const rules: { event: Event; label: string }[] = [
  { event: 'approval', label: 'An agent needs your OK for a write' },
  { event: 'question', label: 'An agent asks you a question' },
  { event: 'andon', label: 'A factory pulls the andon' },
  { event: 'budget', label: 'A budget passes 80%' },
  { event: 'digest', label: 'Daily digest at 08:00' },
];
const channels: { channel: Channel; label: string }[] = [
  { channel: 'in_goal', label: 'In Goal' },
  { channel: 'email', label: 'Email' },
  { channel: 'slack', label: 'Slack' },
];

const saveFailed = 'Goal couldn’t save that. Check your connection and try again.';

/** The table, live with a person; the Notifications section's own body. */
export function NotificationsTable({ org, signedIn }: { org: string; signedIn: boolean }) {
  return signedIn ? <LiveTable org={org} /> : <PreferencesTable />;
}

function LiveTable({ org }: { org: string }) {
  const prefs = useQuery(notificationsQuery);
  const setting = useQuery(orgNotificationsQuery(org));
  const save = useSaveNotifications();

  if (prefs.isPending || setting.isPending) {
    return (
      <Skeleton
        ready={false}
        placeholder={
          <div className="flex flex-col gap-3">
            {rules.map((rule) => (
              <span key={rule.event} className="block h-6 rounded-control bg-hover" />
            ))}
          </div>
        }
      >
        {null}
      </Skeleton>
    );
  }
  if (prefs.isError) {
    return (
      <div className="flex flex-wrap items-center gap-3">
        <span className="text-body">Goal couldn’t load your notification settings.</span>
        <Button variant="white" size="sm" onClick={() => prefs.refetch()}>
          Retry
        </Button>
      </div>
    );
  }

  const change = (event: Event, channel: Channel, on: boolean) => {
    const next: PreferencesChange = (current) => ({
      ...current,
      events: { ...current.events, [event]: { ...current.events[event], [channel]: on } },
    });
    save.mutate(next, { onError: () => toast.error(saveFailed) });
  };

  return <PreferencesTable prefs={prefs.data} slackSet={Boolean(setting.data?.slack_secret)} onChange={change} />;
}

/**
 * The rows and the boxes. Without `prefs` (sign-in off) every box is unchecked and NotAvailable. `onChange`
 * is absent when the boxes cannot be changed.
 */
function PreferencesTable({
  prefs,
  slackSet = false,
  onChange,
}: {
  prefs?: NotificationPreferences;
  slackSet?: boolean;
  onChange?: (event: Event, channel: Channel, on: boolean) => void;
}) {
  const phone = usePhone();

  /** One box of the matrix: live, or the saved value and NotAvailable. `className` sizes the target. */
  const box = (event: Event, channel: Channel, eventLabel: string, channelLabel: string, className?: string) => {
    const boxLabel = `${eventLabel}, ${channelLabel}`;
    const live = prefs !== undefined && (channel !== 'slack' || slackSet);
    const checked = prefs?.events[event][channel] ?? false;
    const extra = className ? { className, label: channelLabel } : {};
    if (live && onChange !== undefined) {
      return (
        <Checkbox
          aria-label={boxLabel}
          {...extra}
          checked={checked}
          onCheckedChange={(on) => onChange(event, channel, on)}
        />
      );
    }
    return (
      // Controlled with a no-op: a click on a box that is not available leaves it at its saved value.
      <NotAvailable label={boxLabel}>
        <Checkbox aria-label={boxLabel} {...extra} checked={checked} onCheckedChange={() => undefined} />
      </NotAvailable>
    );
  };

  // Below 768 px one row per event with a labelled box per channel; the table would be wider than the screen.
  if (phone) {
    return (
      <ul className="m-0 flex list-none flex-col p-0">
        {rules.map(({ event, label }) => (
          <li key={event} className="flex flex-col gap-1 border-t border-line py-3 first:border-t-0 first:pt-0">
            <span className="text-sm">{label}</span>
            {channels.map(({ channel, label: channelLabel }) =>
              event === 'digest' && channel === 'in_goal' ? null : (
                <div key={channel}>{box(event, channel, label, channelLabel, 'min-h-11 w-full')}</div>
              ),
            )}
          </li>
        ))}
      </ul>
    );
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[480px] border-collapse text-sm">
        <thead>
          <tr className="text-left text-meta text-muted [&>th]:pb-2 [&>th]:font-normal">
            <th scope="col">When</th>
            {channels.map((c) => (
              <th key={c.channel} scope="col" className="text-center">
                {c.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rules.map(({ event, label }) => (
            <tr key={event} className="border-t border-line [&>td]:py-3">
              <td>{label}</td>
              {channels.map(({ channel, label: channelLabel }) =>
                event === 'digest' && channel === 'in_goal' ? (
                  <td key={channel} className="text-center text-muted">
                    None
                  </td>
                ) : (
                  <td key={channel} className="text-center">
                    {box(event, channel, label, channelLabel)}
                  </td>
                ),
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

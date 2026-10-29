// The table of the account page's Notifications section (docs/design/screens/account/account.html): which
// events the person is told about, on which channel (ADR 0023). With sign-in the saved preferences come from
// GET /v1/me/notifications, and every box PUTs them whole. The Slack column is live only when the
// organization has a Slack connection; with none its boxes show the saved value and are NotAvailable. With
// sign-in off there is no person, so nothing is requested and every box is NotAvailable.
import { useQuery } from '@tanstack/react-query';
import { toast } from 'sonner';
import { connectionsQuery, notificationsQuery } from '../../api/queries';
import type { NotificationChannels, NotificationEvents, NotificationPreferences } from '../../api/types';
import { Button } from '../../ui/button';
import { NotAvailable } from '../../ui/not-available';
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
  const slack = useQuery(connectionsQuery(org, { kind: 'slack' }));
  const save = useSaveNotifications();

  if (prefs.isPending || slack.isPending) {
    return (
      <div aria-busy="true">
        <span role="status" className="sr-only">
          Loading
        </span>
      </div>
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

  return <PreferencesTable prefs={prefs.data} slackConnected={(slack.data?.length ?? 0) > 0} onChange={change} />;
}

/**
 * The rows and the boxes. Without `prefs` (sign-in off) every box is unchecked and NotAvailable. `onChange`
 * is absent when the boxes cannot be changed.
 */
function PreferencesTable({
  prefs,
  slackConnected = false,
  onChange,
}: {
  prefs?: NotificationPreferences;
  slackConnected?: boolean;
  onChange?: (event: Event, channel: Channel, on: boolean) => void;
}) {
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
              {channels.map(({ channel, label: channelLabel }) => {
                if (event === 'digest' && channel === 'in_goal') {
                  return (
                    <td key={channel} className="text-center text-muted">
                      None
                    </td>
                  );
                }
                const boxLabel = `${label}, ${channelLabel}`;
                const live = prefs !== undefined && (channel !== 'slack' || slackConnected);
                const checked = prefs?.events[event][channel] ?? false;
                const changeable = live && onChange !== undefined;
                return (
                  <td key={channel} className="text-center">
                    {changeable ? (
                      <input
                        type="checkbox"
                        aria-label={boxLabel}
                        checked={checked}
                        onChange={(e) => onChange(event, channel, e.target.checked)}
                        className="size-4"
                      />
                    ) : (
                      // Uncontrolled: a click on a box that is not available is cancelled, which needs the box
                      // to keep its saved value (defaultChecked), not a React-controlled one.
                      <NotAvailable label={boxLabel}>
                        <input type="checkbox" aria-label={boxLabel} defaultChecked={checked} className="size-4" />
                      </NotAvailable>
                    )}
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

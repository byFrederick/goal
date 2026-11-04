// Settings › Notifications: Slack, read-only. Slack sends direct messages through the organization's
// slack_secret (ADR 0037, ADR 0023); the factory has no channel of its own. The secret is chosen in the
// organization's settings, so a person who holds org:manage gets a link there. The row waits in a skeleton
// (MOTION.md, Loading) and a failed read is one line, never a blank.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { orgNotificationsQuery } from '../../api/queries';
import { useOrg, useOrgHolds } from '../../shared/org';
import { Skeleton } from '../../ui/skeleton';

const placeholder = (
  <div className="flex flex-col gap-1">
    <div className="h-4 w-12 rounded-control bg-shell" />
    <div className="h-5 w-64 max-w-full rounded-control bg-shell" />
  </div>
);

export function NotificationsSection() {
  const org = useOrg();
  const mayManage = useOrgHolds('org:manage');
  const settings = useQuery(orgNotificationsQuery(org.id));
  const secret = settings.data?.slack_secret ?? null;
  return (
    <section
      id="notifications"
      aria-labelledby="notifications-title"
      className="rounded-card border border-line bg-card"
    >
      <h2 id="notifications-title" className="m-0 px-4 pt-4 text-base font-medium md:px-5 md:text-lg">
        Notifications
      </h2>
      <div className="@container flex min-w-0 flex-col gap-5 px-4 pt-4 pb-5 md:px-5">
        <Skeleton ready={!settings.isPending} placeholder={placeholder}>
          <div className="flex min-w-0 flex-col gap-1">
            <span className="text-sm font-medium">Slack</span>
            {settings.isError ? (
              <p className="m-0 text-secondary text-rework-ink">Couldn’t load the Slack setting.</p>
            ) : (
              <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2 max-md:[&_a]:inline-flex max-md:[&_a]:min-h-11 max-md:[&_a]:items-center">
                <span className="text-body">
                  {secret ? (
                    <>
                      Sent with the secret <span className="font-mono">{secret}</span>.
                    </>
                  ) : (
                    'Slack is off'
                  )}
                </span>
                {mayManage ? (
                  <Link to="/o/$org/settings" params={{ org: org.slug }} className="font-control text-secondary">
                    Change in organization settings
                  </Link>
                ) : null}
              </div>
            )}
          </div>
        </Skeleton>
      </div>
    </section>
  );
}

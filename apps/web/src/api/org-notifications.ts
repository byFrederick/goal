// The organization's notification settings (ADR 0037): which secret holds the Slack bot token. Not the
// signed-in person's own preferences, which notifications.ts holds.
import { orgPath, unwrap } from '@goal/api-client';
import { cancel, goal } from './client';
import type { NotificationTestResult, OrgNotificationSettings } from './types';

export function getOrgNotificationSettings(org: string, signal?: AbortSignal): Promise<OrgNotificationSettings> {
  return unwrap(goal.GET(orgPath('/settings/notifications'), { params: { path: { org } }, ...cancel(signal) }));
}

/** Sets the Slack secret, or turns Slack off with null (org:manage). 404 secret-not-found, 400 secret-not-allowed. */
export function setOrgNotificationSettings(org: string, slackSecret: string | null): Promise<OrgNotificationSettings> {
  return unwrap(
    goal.PUT(orgPath('/settings/notifications'), {
      params: { path: { org } },
      body: { slack_secret: slackSecret },
    }),
  );
}

/** Asks Slack whether the named secret's token works; a failure is an answer with ok false, not an error. */
export function testOrgNotifications(org: string, slackSecret: string): Promise<NotificationTestResult> {
  return unwrap(
    goal.POST(orgPath('/settings/notifications/test'), {
      params: { path: { org } },
      body: { slack_secret: slackSecret },
    }),
  );
}

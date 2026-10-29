// The signed-in person's notification preferences (ADR 0023, decision 1) over client.ts's goald client.
// They hold in every organization the person belongs to, so no organization is in the path. An API key,
// a personal token and GOAL_AUTH=none's local user are 403 person-required: only a person has them.
import { unwrap } from '@goal/api-client';
import { cancel, goal } from './client';
import type { NotificationPreferences } from './types';

/** The UTC default a person has until they save a time zone (domain.DefaultTimeZone). */
const defaultTimeZone = 'UTC';

/** The browser's IANA time zone, or UTC when the browser does not name one. */
export function browserTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || defaultTimeZone;
}

/** The caller's preferences; a person who never saved any reads the defaults. */
export function getNotificationPreferences(signal?: AbortSignal): Promise<NotificationPreferences> {
  return unwrap(goal.GET('/v1/me/notifications', cancel(signal)));
}

/**
 * Replaces every field. While the saved time zone is still UTC (nothing was saved), the browser's zone is
 * sent instead, so the daily digest goes out at 08:00 where the person is; the screen has no time zone
 * picker. 400 invalid-notification-email and invalid-notification-preferences come back as ApiError.
 */
export function putNotificationPreferences(prefs: NotificationPreferences): Promise<NotificationPreferences> {
  const timeZone = prefs.time_zone === defaultTimeZone ? browserTimeZone() : prefs.time_zone;
  return unwrap(goal.PUT('/v1/me/notifications', { body: { ...prefs, time_zone: timeZone } }));
}

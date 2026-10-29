// The save of the person's notification preferences, shared by the table's boxes and the profile's email.
// Each change is built from the cached preferences when it runs, not when it is asked for, and the
// mutations run one at a time (the scope), so one PUT never reverts another's change. A failure leaves the
// cache as it was: the box or the email keeps its saved value.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { putNotificationPreferences } from '../../api/notifications';
import { keys } from '../../api/queries';
import type { NotificationPreferences } from '../../api/types';

export type PreferencesChange = (current: NotificationPreferences) => NotificationPreferences;

export function useSaveNotifications() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationKey: keys.notifications,
    scope: { id: 'notification-preferences' },
    mutationFn: (change: PreferencesChange) => {
      const current = queryClient.getQueryData<NotificationPreferences>(keys.notifications);
      if (!current) throw new Error('The notification preferences are not loaded');
      return putNotificationPreferences(change(current));
    },
    onSuccess: (saved) => queryClient.setQueryData(keys.notifications, saved),
  });
}

// The state of one trigger's switch: what it shows, whether people may change it, and the write behind it.
// The switch shows the value it asked for while the write runs, and the stored value again when it fails
// (the stream's `definition` event and the refetch below then bring the truth).
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { defKeys, keys } from '../../api/queries';
import { useFactory } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { holds } from '../../shared/permissions';
import { setTriggerEnabled } from './set-trigger-enabled';

/** Said instead of a change while a draft is open: the draft is what Definition publishes, not the switch. */
export const draftOpenNotice = 'Publish or discard the open draft in Definition first. The trigger is unchanged.';

export function useTriggerToggle(name: string, enabled: boolean) {
  const org = useOrg();
  const factory = useFactory();
  const queryClient = useQueryClient();
  const [notice, setNotice] = useState<string | null>(null);
  const canEdit = holds(factory, 'definition:edit');

  const write = useMutation({
    mutationFn: (next: boolean) =>
      setTriggerEnabled({
        org: org.id,
        factory: factory.id,
        currentVersionId: factory.current?.id ?? null,
        trigger: name,
        enabled: next,
      }),
    onSuccess: () => setNotice(null),
    onError: (error) => setNotice(`Not changed. ${error instanceof Error ? error.message : 'Try again.'}`),
    // Waits for the refetch, so the switch shows the stored value again without a flicker back.
    onSettled: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: keys.intake(org.id, factory.id) }),
        queryClient.invalidateQueries({ queryKey: defKeys.factories(org.id) }),
      ]);
    },
  });

  const pending = write.isPending;
  return {
    checked: pending ? (write.variables ?? enabled) : enabled,
    disabled: !canEdit || pending,
    notice,
    toggle(next: boolean) {
      if (factory.draft !== null) {
        setNotice(draftOpenNotice);
        return;
      }
      setNotice(null);
      write.mutate(next);
    },
  };
}

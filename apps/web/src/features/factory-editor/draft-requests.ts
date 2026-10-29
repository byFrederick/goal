// Asked definition changes (ADR 0021) as the screens use them: the one mutation that asks, the words a
// refusal shows, and which requests are still going. The Ask bar (yaml/ask-bar.tsx) and Ask Jonah for
// a fix (features/agents) both ask through useAskFactory, so both refresh and refuse the same way.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { ApiError } from '../../api/client';
import { createDraftRequest } from '../../api/definitions';
import { defKeys, draftRequestQuery } from '../../api/queries';
import type { DraftRequest, NewDraftRequest } from '../../api/types';
import { useFactory } from '../../shared/factory';
import { useOrg } from '../../shared/org';

/** A request that has not ended: goald refuses another one until it does (409 draft-request-open). */
export function isOpenRequest(request: Pick<DraftRequest, 'status'> | undefined): boolean {
  return request?.status === 'queued' || request?.status === 'running';
}

/** Why an ask was refused, in plain words. A 409 yaml-invalid is not the already-running refusal: it names the parse problem. */
export function askFailure(err: unknown): string {
  if (!(err instanceof ApiError)) return 'goald is not answering. Nothing was asked; try again once it is.';
  if (err.code === 'draft-request-open') return 'A request is already running. Wait for it to finish, then ask again.';
  if (err.code === 'draft-request-text-invalid') return 'Write what to change, in up to 2000 characters.';
  return `Not asked: ${err.detail ?? err.title}`;
}

/**
 * Asks for a change to the factory's definition. The new request is shown at once, from the answer;
 * the list is read again whether the ask worked or was refused, so a 409 shows the request that runs.
 */
export function useAskFactory() {
  const org = useOrg();
  const factory = useFactory();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (request: NewDraftRequest) => createDraftRequest(org.id, factory.id, request),
    onSuccess: (created) => {
      queryClient.setQueryData(draftRequestQuery(org.id, factory.id, created.id).queryKey, created);
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: defKeys.draftRequests(org.id, factory.id) }),
  });
}

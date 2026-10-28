// Creating an organization (ADR 0032 decision 1): the request, then the organization list refreshed
// before the caller goes to it. The organization dialog and onboarding's first step both create
// through it; each decides where the person goes next.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { ApiError, createOrg } from '../api/client';
import { keys } from '../api/queries';
import type { NewOrg, Org } from '../api/types';

export function useCreateOrg(onCreated: (org: Org) => void | Promise<void>) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (input: NewOrg) => createOrg(input),
    onSuccess: async (org) => {
      // The /o/$org layout resolves the address from the organization list: refresh it first.
      await client.invalidateQueries({ queryKey: keys.orgs, refetchType: 'all' });
      await onCreated(org);
    },
  });
}

/** Whether goald refused the address because another organization already has it. */
export function isSlugTaken(error: unknown): boolean {
  return error instanceof ApiError && error.code === 'slug-taken';
}

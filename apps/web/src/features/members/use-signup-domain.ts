// The "Only emails at a domain" switch (ADR 0023, decision 7): the organization's sign-up domain,
// set to the owner's own email domain or cleared. It reads the settings, and a change refreshes
// them, so the switch shows what goald stored and not what was clicked. A change that needs a
// recent sign-in is asked for and sent once more by the API client (api/session.ts).
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ApiError, clearSignupDomain, setSignupDomain } from '../../api/client';
import { keys, settingsQuery } from '../../api/queries';
import type { OrgSummary } from '../../api/types';
import { refusal } from './use-people';

/** The domain of a sign-in email, which is what the switch sets when it is turned on. */
export function domainOf(email: string | undefined): string | undefined {
  return email?.split('@')[1]?.trim().toLowerCase() || undefined;
}

/** goald's refusals of a sign-up domain, in the page's words. */
const signupRefusals: Record<string, string> = {
  'signup-domain-not-yours': 'Your sign-in email is not at that domain, so you cannot turn this on for it.',
  'signup-domain-public': 'A public email provider cannot be a sign-up domain. Use a company domain.',
  'signup-domain-taken': 'Another organization already uses this domain for sign-up.',
};

/** A refused change to the sign-up domain, in the page's words. */
export function signupFailure(err: unknown, org: string): string {
  const code = err instanceof ApiError ? err.problem.code : undefined;
  return (code && signupRefusals[code]) || refusal(err, org);
}

export function useSignupDomain(org: OrgSummary, email: string | undefined) {
  const client = useQueryClient();
  const settings = useQuery(settingsQuery(org.id));
  const refresh = () => client.invalidateQueries({ queryKey: keys.settings(org.id) });
  const set = useMutation({
    mutationFn: (domain: string) => setSignupDomain(org.id, { domain, join_role: 'viewer' }),
    onSuccess: refresh,
  });
  const clear = useMutation({
    mutationFn: () => clearSignupDomain(org.id),
    onSuccess: refresh,
  });
  const turn = (on: boolean) => {
    if (!on) {
      clear.mutate();
      return;
    }
    const domain = domainOf(email);
    if (domain) set.mutate(domain);
  };
  const failed = set.error ?? clear.error;
  return {
    settings,
    stored: settings.data?.signup_domain ?? null,
    busy: set.isPending || clear.isPending,
    failure: failed ? signupFailure(failed, org.name) : undefined,
    turn,
  };
}

// The signed-in person's own actions (spec 3 §10.2 Account-Menu): sign out, sign out everywhere,
// change one's name. The identity service's client is loaded only when one of them runs.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { toast } from 'sonner';
import { keys } from '../api/queries';
import type { OrgRole } from '../api/types';

const identityService = () => import('../api/auth');

const unreachable = 'Can’t reach Goal. Check your connection and try again.';

/** Sign out here, or everywhere: then nothing read before is kept, and the sign-in page opens. */
export function useSignOut() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const leave = async () => {
    queryClient.clear();
    await navigate({ to: '/sign-in', replace: true });
  };
  const signOut = useMutation({
    mutationKey: ['auth', 'sign-out'],
    mutationFn: async () => (await identityService()).signOut(),
    onSuccess: leave,
    onError: () => toast.error(unreachable),
  });
  const everywhere = useMutation({
    mutationKey: ['auth', 'sign-out', 'everywhere'],
    mutationFn: async () => (await identityService()).signOutEverywhere(),
    onSuccess: leave,
  });
  return { signOut, everywhere };
}

/** Changes the person's name; the account menu and the members list read it again. */
export function useUpdateName() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationKey: ['auth', 'update-name'],
    mutationFn: async (name: string) => (await identityService()).updateName(name),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: keys.me }),
  });
}

/** Keeps the session alive: Better Auth extends it only when its own endpoints see it (spec 3 §8.3). */
export async function keepSessionAlive(): Promise<void> {
  await (await identityService()).keepSessionAlive();
}

/** "FC" for F. Cruz, "AO" for a.ortiz: the first letters of the first and last words. */
export function initialsOf(name: string): string {
  const words = name.split(/[^\p{L}\p{N}]+/u).filter(Boolean);
  const first = words[0]?.[0] ?? '?';
  const last = words.length > 1 ? (words.at(-1)?.[0] ?? '') : '';
  return `${first}${last}`.toUpperCase();
}

const roleNames: Record<OrgRole, string> = { owner: 'Owner', admin: 'Admin', member: 'Member' };

/** "Owner of Acme"; undefined without a role (sign-in off, an API key). */
export function roleLine(role: OrgRole | null | undefined, orgName: string): string | undefined {
  return role ? `${roleNames[role]} of ${orgName}` : undefined;
}

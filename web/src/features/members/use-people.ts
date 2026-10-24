// The changes Members and roles makes: membership through the identity service (invite, resend,
// cancel, change a role, remove, leave: Better Auth's endpoints, whose owner rules apply) and
// factory roles through goald. Each refreshes the members and invitations; the identity service's
// client is loaded only when one runs. A change that needs a recent sign-in asks to confirm it's
// you and is sent once more (api/auth.ts, api/session.ts).
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useRouter } from '@tanstack/react-router';
import { toast } from 'sonner';
import type { AuthFailure, InviteRequest, OrgRoleName } from '../../api/auth';
import { ApiError, setFactoryRole } from '../../api/client';
import { keys } from '../../api/queries';
import type { FactoryRole, Invitation, Member, OrgSummary } from '../../api/types';

const identityService = () => import('../../api/auth');

const unreachable = 'Can’t reach Goal. Check your connection and try again.';

/** A refused change in the page's words. */
export function refusal(err: unknown, org: string): string {
  if (err instanceof ApiError) return err.detail ?? err.title;
  const kind = (err as Partial<AuthFailure>)?.kind;
  switch (kind) {
    case 'unreachable':
      return unreachable;
    case 'owners-only':
      return 'Only owners change, invite or remove an owner.';
    case 'already-member':
      return `Already a member of ${org}.`;
    case 'only-owner':
      return `${org} needs at least one owner. Make someone else an owner first.`;
    case 'rate-limited':
      return 'Too many tries. Try again in a few minutes.';
    case 'reauth-required':
      return 'Not changed: this needs a recent sign-in.';
    default:
      return 'Goal refused the change. Reload the page and try again.';
  }
}

const role = { owner: 'an owner', admin: 'an admin', member: 'a member' } as const;

export function usePeopleChanges(org: OrgSummary) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const router = useRouter();
  const refresh = () =>
    Promise.all([
      client.invalidateQueries({ queryKey: keys.members(org.id) }),
      client.invalidateQueries({ queryKey: keys.invitations(org.id) }),
    ]);
  /** The caller's own role changed: the organization, its list and what they may do, too. */
  const refreshMine = async () => {
    await Promise.all([
      refresh(),
      client.invalidateQueries({ queryKey: keys.detail(org.id) }),
      client.invalidateQueries({ queryKey: keys.orgs }),
    ]);
    // The sidebar and the menu read the role from the route's organization.
    await router.invalidate();
  };
  const failed = (err: unknown) => toast.error(refusal(err, org.name));

  const changeRole = useMutation({
    mutationFn: async ({ member, to }: { member: Member; to: OrgRoleName }) =>
      (await identityService()).updateMemberRole(org.id, member.member_id, to),
    onSuccess: async (_, { member, to }) => {
      await (member.you ? refreshMine() : refresh());
      toast.success(member.you ? `You’re now ${role[to]} of ${org.name}` : `${member.user.name} is now ${role[to]}`);
    },
    onError: failed,
  });

  const remove = useMutation({
    mutationFn: async (member: Member) => (await identityService()).removeMember(org.id, member.member_id),
    onSuccess: async (_, member) => {
      await refresh();
      toast.success(`Removed ${member.user.name} from ${org.name}`);
    },
  });

  const leave = useMutation({
    mutationFn: async () => (await identityService()).leaveOrganization(org.id),
    onSuccess: async () => {
      client.setQueryData<OrgSummary[]>(keys.orgs, (orgs) => orgs?.filter((o) => o.id !== org.id));
      toast.success(`You left ${org.name}`);
      await navigate({ to: '/', replace: true });
      client.removeQueries({ queryKey: keys.org(org.id) });
      void client.invalidateQueries({ queryKey: keys.orgs });
    },
  });

  const invite = useMutation({
    mutationFn: async (requests: InviteRequest[]) => {
      const auth = await identityService();
      const refused: { email: string; err: unknown }[] = [];
      // One at a time, so a refusal names its address and the rest still go.
      for (const r of requests) {
        try {
          await auth.invite(r);
        } catch (err) {
          refused.push({ email: r.email, err });
        }
      }
      return refused;
    },
    onSettled: refresh,
  });

  const resend = useMutation({
    mutationFn: async (i: Invitation) =>
      (await identityService()).invite({
        organizationId: org.id,
        email: i.email,
        role: i.role,
        factoryRoles: i.factory_roles.map((g) => ({ factoryId: g.factory_id, role: g.role })),
        resend: true,
      }),
    onSuccess: async (_, i) => {
      await refresh();
      toast.success(`Sent the invitation to ${i.email} again`);
    },
    onError: failed,
  });

  const cancel = useMutation({
    mutationFn: async (i: Invitation) => (await identityService()).cancelInvitation(i.id),
    onSuccess: async (_, i) => {
      await refresh();
      toast.success(`Cancelled the invitation to ${i.email}`);
    },
    onError: failed,
  });

  /** Gives or takes a factory role at once (the cache first), back as it was if goald refuses. */
  const factoryRole = useMutation({
    mutationFn: ({ member, factory, to }: { member: Member; factory: string; to: FactoryRole | null }) =>
      setFactoryRole(org.id, member.user.id, factory, to),
    onMutate: async ({ member, factory, to }) => {
      await client.cancelQueries({ queryKey: keys.members(org.id) });
      const before = client.getQueryData<Member[]>(keys.members(org.id));
      client.setQueryData<Member[]>(keys.members(org.id), (list) =>
        list?.map((m) =>
          m.user.id !== member.user.id
            ? m
            : {
                ...m,
                factory_roles: [
                  ...m.factory_roles.filter((g) => g.factory_id !== factory),
                  ...(to ? [{ factory_id: factory, role: to }] : []),
                ],
              },
        ),
      );
      return { before };
    },
    onError: (err, _, context) => {
      if (context?.before) client.setQueryData(keys.members(org.id), context.before);
      failed(err);
    },
    onSettled: () => client.invalidateQueries({ queryKey: keys.members(org.id) }),
  });

  return { changeRole, remove, leave, invite, resend, cancel, factoryRole };
}

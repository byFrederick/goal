// Basics › Who approves (spec 3 §10.2, canvas Define-Basics and New-Basics): read-only, since who
// approves is not part of the definition but of the factory's roles. Owners and admins of the
// organization approve everywhere; a factory's approvers and editors approve in it, named from
// Members (listMembers). A new factory has none of its own yet, so its owners and admins approve
// until someone is given the role. Whoever manages people gets a link to change it in Members.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { membersQuery } from '../../../api/queries';
import type { Member } from '../../../api/types';
import { displayName } from '../../../shared/names';
import { useOrg, useOrgHolds } from '../../../shared/org';

/** Who approves in factory `factory`: owners and admins, and its approvers and editors, by name. */
export function approvers(members: readonly Member[], factory: string | undefined): string[] {
  return members
    .filter(
      (m) =>
        m.role === 'owner' ||
        m.role === 'admin' ||
        m.factory_roles.some((g) => g.factory_id === factory && (g.role === 'approver' || g.role === 'editor')),
    )
    .map((m) => displayName(m.user))
    .sort((a, b) => a.localeCompare(b));
}

export function WhoApproves({ place, factory }: { place: 'definition' | 'wizard'; factory?: string | undefined }) {
  const org = useOrg();
  const manages = useOrgHolds('member:manage');
  const { data: members = [] } = useQuery(membersQuery(org.id));
  const names = approvers(members, factory);
  return (
    <div className="col-span-full flex min-w-0 flex-col gap-1.5">
      <h3 className="m-0 text-sm font-medium">Who approves</h3>
      <p className="m-0 text-[15px] leading-normal">
        {place === 'wizard'
          ? 'Owners and admins approve until you add approvers in Members.'
          : `Owners and admins of ${org.name}, and this factory’s approvers and editors${names.length > 0 ? `: ${names.join(', ')}.` : '.'}`}
      </p>
      <p className="m-0 text-[13px] leading-snug text-muted">
        Human stations and write actions wait for one of them. Approvers and editors are factory roles, set per person
        in Members and roles.
        {manages ? (
          <>
            {' '}
            <Link to="/o/$org/members" params={{ org: org.slug }} className="text-ink underline underline-offset-2">
              Change in Members
            </Link>
          </>
        ) : null}
      </p>
    </div>
  );
}

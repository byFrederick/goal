// People with access: the organization's members and the organization role each holds. Factory roles are
// not built yet, so the role control and "Add people" are disabled (NotAvailable, ADR 0016 decision 4).
import { useQuery } from '@tanstack/react-query';
import { membersQuery } from '../../api/queries';
import type { Member, OrgSummary } from '../../api/types';
import { Button } from '../../ui/button';
import { SectionCard } from '../../ui/card';
import { NotAvailable } from '../../ui/not-available';
import { orgRoleNames } from '../members/people';

/** The two letters on a member's avatar: the first letter of the first two words of the name. */
function initials(name: string): string {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((word) => word[0])
    .join('')
    .toUpperCase();
}

function MemberRow({ member }: { member: Member }) {
  return (
    <div className="flex items-center gap-3 border-t border-line pt-3">
      <span
        aria-hidden="true"
        className="grid size-8 shrink-0 place-items-center rounded-full bg-hover text-meta font-control text-ink-soft"
      >
        {initials(member.user.name)}
      </span>
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="font-control">{member.user.name}</span>
        <span className="text-meta text-muted">{member.user.email}</span>
      </span>
      <NotAvailable label="Factory roles">
        <Button size="sm" variant="secondary">
          {orgRoleNames[member.role]}
        </Button>
      </NotAvailable>
    </div>
  );
}

export function PeopleCard({ org }: { org: OrgSummary }) {
  const members = useQuery(membersQuery(org.id));

  return (
    <SectionCard id="people" title="People with access" aria-busy={members.isPending || undefined}>
      {members.isPending ? <span className="sr-only">Loading</span> : null}
      {members.isError ? (
        <p role="alert" className="m-0 text-secondary text-rework-ink">
          goald is not answering. Try again once it is running.
        </p>
      ) : null}
      {members.data?.length === 0 ? <p className="m-0 text-secondary text-muted">No people with access yet.</p> : null}
      {members.data?.map((member) => (
        <MemberRow key={member.member_id} member={member} />
      ))}
      <div className="border-t border-line pt-3">
        <NotAvailable label="Add people">
          <Button size="sm" variant="secondary">
            Add people
          </Button>
        </NotAvailable>
      </div>
    </SectionCard>
  );
}

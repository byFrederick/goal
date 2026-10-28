// The panel's People card (docs/design/screens/get-started/invite.jpg): the caller as "You, owner" and each
// pending invitation as "<name>, <role>, invited". Both come from the same queries Members reads, so an invite
// sent on this step shows here once the invitations refresh.
import { useQuery } from '@tanstack/react-query';
import { invitationsQuery, membersQuery } from '../../api/queries';
import type { OrgSummary } from '../../api/types';
import { initialsOf } from '../../shared/names';
import { Avatar } from '../../ui/mark';
import { inviteRoleWord } from './people-roles';

export function PeopleCard({ org }: { org: OrgSummary }) {
  const members = useQuery(membersQuery(org.id));
  const invitations = useQuery(invitationsQuery(org.id));

  if (members.isError || invitations.isError) {
    return (
      <section aria-label="People" className="flex min-w-0 flex-col gap-2 rounded-card border border-andon-line p-4">
        <span className="text-meta text-andon-text">People</span>
        <span className="text-secondary text-andon-text">The people can’t be read.</span>
      </section>
    );
  }
  if (members.isPending || invitations.isPending) {
    return (
      <section aria-label="People" aria-busy="true" className="min-w-0 rounded-card border border-andon-line p-4">
        <span role="status" className="sr-only">
          Loading
        </span>
      </section>
    );
  }

  const you = members.data.find((m) => m.you);
  // The invitations list holds only the pending ones.
  const pending = invitations.data;
  const lines = [
    ...(you ? [{ key: you.member_id, text: `You, ${you.role}` }] : []),
    ...pending.map((i) => ({
      key: i.id,
      text: `${i.email.split('@')[0] ?? i.email}, ${inviteRoleWord(i)}, invited`,
    })),
  ];
  const people = [...(you ? [you.user] : []), ...pending.map((i) => ({ name: '', email: i.email }))];

  return (
    <section
      aria-label="People"
      className="flex min-w-0 flex-col gap-3 rounded-card border border-andon-line bg-andon-raised p-4"
    >
      <span className="text-meta text-andon-text">People</span>
      <ul className="m-0 flex list-none gap-1 p-0" aria-hidden="true">
        {people.map((p) => (
          <li key={p.email}>
            <Avatar initials={initialsOf(p)} size="sidebar" />
          </li>
        ))}
      </ul>
      <ul className="m-0 flex list-none flex-col gap-1 p-0">
        {lines.map((line) => (
          <li key={line.key} className="text-secondary break-words text-white">
            {line.text}
          </li>
        ))}
      </ul>
    </section>
  );
}

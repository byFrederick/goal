// A person named by an actor ref ("user:<id>") as the members list names them. A ref that is not a person
// reads as it is; a person not in the list (or a list the viewer may not read) reads as "A person".
import { useQuery } from '@tanstack/react-query';
import { membersQuery } from '../../api/queries';
import { useOrg } from '../../shared/org';

/** A function from an actor ref to the name to show. */
export function useActorName(): (ref: string) => string {
  const org = useOrg();
  const members = useQuery(membersQuery(org.id));
  return (ref) => {
    if (!ref.startsWith('user:')) return ref === '' ? 'Jonah' : ref.replace(/^[a-z]+:/, '');
    const id = ref.slice('user:'.length);
    return members.data?.find((m) => m.user.id === id)?.user.name ?? 'A person';
  };
}

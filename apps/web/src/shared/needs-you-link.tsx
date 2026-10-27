// The "Needs you" link in a factory's header (the Board and the Floor): what waits on people in this
// factory, leading to the Needs you page filtered to it. The inbox query is already limited to the
// factory, so its entries are the page's All tab for it.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { inboxQuery } from '../api/queries';
import type { Factory } from '../api/types';
import { Button } from '../ui/button';
import { Count } from '../ui/count';
import { useOrg } from './org';

/** The header link to the Needs you page for one factory, with the count of what waits on you there. */
export function NeedsYouLink({ factory }: { factory: Factory }) {
  const org = useOrg();
  const { data } = useQuery(inboxQuery(org.id, factory.id));
  const count = data?.entries.length ?? 0;

  return (
    <Button variant="raised" asChild>
      <Link to="/o/$org/needs-you" params={{ org: org.slug }} search={{ factory: factory.name }}>
        Needs you
        {count > 0 ? <Count value={count} label=" waiting on you" tone="you" /> : null}
      </Link>
    </Button>
  );
}

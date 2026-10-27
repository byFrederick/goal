// The "Needs you" link in a factory's header (the Board and the Floor): what waits on people in this
// factory, leading to the Needs you page filtered to it. Its count is the page's All tab for the
// factory, so the two agree.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { inboxQuery } from '../api/queries';
import type { Factory } from '../api/types';
import { tabCounts } from '../features/needs-you/search';
import { Button } from '../ui/button';
import { Count } from '../ui/count';
import { useOrg } from './org';

/** The header link to the Needs you page for one factory, with the count of what waits on you there. */
export function NeedsYouLink({ factory }: { factory: Factory }) {
  const org = useOrg();
  const { data } = useQuery(inboxQuery(org.id, factory.id));
  const count = tabCounts(data?.entries ?? [], factory.name).all;

  return (
    <Button variant="raised" asChild>
      <Link to="/o/$org/needs-you" params={{ org: org.slug }} search={{ factory: factory.name }}>
        Needs you
        {count > 0 ? <Count value={count} label=" waiting on you" tone="you" /> : null}
      </Link>
    </Button>
  );
}

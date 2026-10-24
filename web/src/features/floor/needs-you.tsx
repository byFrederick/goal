// The Floor's Needs you card (Main): what of this factory waits on a person, from the
// organization's inbox. Decisions are one row that leads to them; every other entry (a stopped
// line, a gate near its rework rate, blocked and expedited work) a row of its own.
import { useSuspenseQuery } from '@tanstack/react-query';
import { inboxQuery } from '../../api/queries';
import type { Factory } from '../../api/types';
import { plural } from '../../shared/format';
import { needRows } from '../../shared/needs-you';
import { NeedRows } from '../../shared/needs-you-list';
import { useOrg } from '../../shared/org';
import { SectionCard } from '../../ui/card';

export function NeedsYouCard({ factory, className }: { factory: Factory; className?: string }) {
  const org = useOrg();
  const { data: inbox } = useSuspenseQuery(inboxQuery(org.id, factory.id));
  const rows = needRows(inbox.entries, Date.now());

  return (
    <SectionCard
      title="Needs you"
      description={rows.length > 0 ? `${plural(rows.length, 'thing')} right now` : 'Nothing right now'}
      className={className}
    >
      {rows.length === 0 ? (
        <p className="m-0 border-t border-line pt-3 text-[15px] text-muted">
          Nothing waits on you. Approvals, stops and blocked work show up here as they happen.
        </p>
      ) : (
        <NeedRows rows={rows} className="-mt-1 border-t border-line" />
      )}
    </SectionCard>
  );
}

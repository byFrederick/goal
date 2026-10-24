// The phone's Items view opens with what waits on you (Phone board): a sentence of the decisions,
// the oldest one expanded to decide on the spot, the rest of what waits as rows, and how the line
// is doing. Phones only; the list of items follows.
import { useSuspenseQuery } from '@tanstack/react-query';
import { boardQuery, inboxQuery } from '../../api/queries';
import type { Board, Factory } from '../../api/types';
import { humanize } from '../../shared/format';
import { decisionsSentence, needRow } from '../../shared/needs-you';
import { ApprovalCard, NeedRows } from '../../shared/needs-you-list';
import { useOrg } from '../../shared/org';

/** "Line running. 9 in progress, constraint at Investigate." */
export function lineSentence(board: Board): string {
  const status = board.line.status === 'stopped' ? 'Line stopped' : 'Line running';
  const constraint = board.constraint ? `, constraint at ${humanize(board.constraint)}` : '';
  return `${status}. ${board.line.wip.in_progress} in progress${constraint}.`;
}

export function PhoneNeedsYou({ factory }: { factory: Factory }) {
  const org = useOrg();
  const { data: inbox } = useSuspenseQuery(inboxQuery(org.id, factory.id));
  const { data: board } = useSuspenseQuery(boardQuery(org.id, factory.id));
  const now = Date.now();

  const decisions = inbox.entries.filter((e) => e.kind === 'decision');
  const [first, ...rest] = decisions;
  const others = inbox.entries.filter((e) => e.kind !== 'decision');
  const rows = [...rest, ...others].map((e) => needRow(e, now));
  const station = factory.stations.find((s) => s.id === first?.station);

  return (
    <section aria-labelledby="needs-you-h" className="flex flex-col gap-2.5 md:hidden">
      <h2 id="needs-you-h" className="m-0 text-[30px] leading-tight font-normal tracking-display">
        Needs you
      </h2>
      <p className="m-0 text-[15px] text-muted">
        {decisions.length > 0 ? `${decisionsSentence(decisions)}.` : 'Nothing waits for your OK.'}
      </p>
      {first ? <ApprovalCard entry={first} station={station} headingLevel={3} /> : null}
      {rows.length > 0 ? <NeedRows rows={rows} className="rounded-card border border-line bg-card px-3" /> : null}
      <p role="status" className="m-0 text-sm text-muted">
        {lineSentence(board)}
      </p>
    </section>
  );
}

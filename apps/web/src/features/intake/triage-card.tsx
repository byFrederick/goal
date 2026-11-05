// Follow-ups to triage (Intake, ADR 0040 decision 9): the follow-ups that name no item and no time. The
// collecting triage item holds those that have landed; the items below it hold more that join it when they land.
import { Link } from '@tanstack/react-router';
import { CornerDownRight } from 'lucide-react';
import type { ReactNode } from 'react';
import type { IntakeTriage } from '../../api/types';
import { refParam, useFactoryParams } from '../../shared/factory';
import { humanize } from '../../shared/format';
import { RefLink } from '../../shared/ref-link';
import { Button } from '../../ui/button';
import { Panel, rowClass } from './panel';

// A link inside a line of text needs more than colour to be told apart (WCAG 1.4.1).
const refLinkClass = 'font-medium text-link underline underline-offset-2';

const info =
  'Follow-ups that name no item and no time. When an item lands they join the open triage item, which the architect turns into work or closes.';

export function TriageCard({ triage }: { triage: IntakeTriage | null | undefined }) {
  const params = useFactoryParams();
  const count = triage ? (triage.item?.follow_ups ?? 0) + triage.waiting.reduce((n, w) => n + w.follow_ups, 0) : 0;
  return (
    <Panel title="Follow-ups to triage" info={info} aside={<span className="tabular-nums">{count}</span>}>
      {!triage ? (
        <p className="m-0 border-t border-line px-5 py-4 text-body text-muted">No follow-ups are waiting for triage.</p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {triage.item ? (
            <li className={rowClass}>
              <Count n={triage.item.follow_ups} word="follow-ups" />
              <span className="flex min-w-0 flex-col">
                <span className="truncate font-medium" title={triage.item.title}>
                  <RefLink item={triage.item.ref} className={refLinkClass} /> {triage.item.title}
                </span>
                <Sub>In {humanize(triage.item.station)}, for the architect</Sub>
              </span>
              <Button asChild variant="secondary" size="sm">
                <Link to="/o/$org/f/$factory/items/$ref" params={{ ...params, ref: refParam(triage.item.ref) }}>
                  Open
                </Link>
              </Button>
            </li>
          ) : null}
          {triage.waiting.map((source) => (
            <li key={source.id} className={rowClass}>
              <Count n={source.follow_ups} word="waiting" />
              <span className="flex min-w-0 flex-col">
                <span className="truncate font-medium" title={source.title}>
                  <RefLink item={source.ref} className={refLinkClass} /> {source.title}
                </span>
                <Sub>Join it when {source.ref} lands</Sub>
              </span>
              <span />
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

function Count({ n, word }: { n: number; word: string }) {
  return (
    <span className="col-span-2 flex flex-col text-body font-medium tabular-nums md:col-span-1">
      {n}
      <span className="text-secondary font-normal text-muted">{word}</span>
    </span>
  );
}

function Sub({ children }: { children: ReactNode }) {
  return (
    <span className="flex min-w-0 items-center gap-2 text-secondary text-muted">
      <CornerDownRight size={12} strokeWidth={1.75} aria-hidden="true" className="flex-none" />
      <span className="min-w-0 break-words">{children}</span>
    </span>
  );
}

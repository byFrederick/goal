// Tried on past items (proposal.jpg, ADR 0039 decisions 10 to 13): each item the proposal was tried on with its
// verdict and the reason, the kept tests the station holds with their result, and what the tries cost and took. A model
// proposal shows its three tries per item beside the item's line result instead (ModelTries).
import { useQuery } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { keptTestsQuery } from '../../api/queries';
import type { ProposalTried, TriedItem, TriedTry } from '../../api/types';
import { useFactory } from '../../shared/factory';
import { formatPercent, formatUsd } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { SectionCard } from '../../ui/card';
import { Chip, type ChipTone } from '../../ui/chip';
import { InfoTip } from './info-tip';
import {
  durationText,
  itemNumber,
  itemReason,
  itemVerdict,
  latestTry,
  passedFirstTime,
  verdictLabels,
  verdictTones,
} from './jonah-format';

/** What a try's chip says: its verdict, or how far it is while it has none. */
function tryChip(t: TriedTry | undefined): { label: string; tone: ChipTone } {
  if (t?.verdict) return { label: verdictLabels[t.verdict], tone: verdictTones[t.verdict] };
  if (t?.state === 'running') return { label: 'Trying', tone: 'run' };
  return { label: 'Queued', tone: 'plain' };
}

/** "41 min, $8.40": how long the tries took, once all are done, and what they cost. */
export function triedAside(tried: ProposalTried): string {
  const cost = formatUsd(tried.cost_usd);
  return tried.seconds === undefined ? cost : `${durationText(tried.seconds)}, ${cost}`;
}

function ItemRow({ item }: { item: TriedItem }) {
  const chip = tryChip(latestTry(item));
  const attempts = item.tries.length;
  const reason = itemReason(item);
  return (
    <li className="flex flex-col gap-1 border-t border-line py-3 first:border-t-0">
      <div className="flex items-center justify-between gap-3">
        <span className="min-w-0 break-words">
          <span className="mr-2 text-muted tabular-nums">{itemNumber(item.item_ref)}</span>
          <span className="text-ink">{item.item_title}</span>
        </span>
        <span className="flex shrink-0 items-center gap-2">
          {attempts > 1 ? (
            <span className="text-secondary text-muted tabular-nums">{`Try ${attempts} of ${attempts}`}</span>
          ) : null}
          <Chip tone={chip.tone}>{chip.label}</Chip>
        </span>
      </div>
      {reason && itemVerdict(item) !== 'fixed' ? (
        <span className="text-secondary text-muted break-words">{reason}</span>
      ) : null}
    </li>
  );
}

/** The Tried on past items card of a definition proposal. */
export function TriedPanel({ tried, action }: { tried: ProposalTried; action?: ReactNode }) {
  const org = useOrg();
  const factory = useFactory();
  const kept = useQuery(keptTestsQuery(org.id, factory.id));
  const titleOf = (test: number) => kept.data?.tests.find((k) => k.item.number === test)?.item.title;
  const notTried = tried.state === 'budget_used' && tried.items.every((item) => itemVerdict(item) === undefined);
  return (
    <SectionCard
      title={
        <span className="inline-flex items-center gap-1">
          Tried on past items
          <InfoTip label="How tries work">
            Goal reruns past items with this change and checks whether what sent each one back is fixed.
          </InfoTip>
        </span>
      }
      aside={
        <span className="flex flex-wrap items-center justify-end gap-x-3 gap-y-1">
          <span className="text-secondary text-muted">{triedAside(tried)}</span>
          {action}
        </span>
      }
    >
      {notTried ? (
        <p className="m-0 text-secondary text-muted">Not tried: budget used</p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {tried.items.map((item) => (
            <ItemRow key={item.item_ref} item={item} />
          ))}
        </ul>
      )}
      {tried.state === 'budget_used' && !notTried ? (
        <p className="m-0 text-secondary text-muted">Some items were not tried: budget used</p>
      ) : null}
      {tried.grader ? (
        <p className="m-0 text-secondary text-muted">
          {`The grader read ${formatPercent(tried.grader.original_hit_rate)} of the original results and ${formatPercent(tried.grader.final_hit_rate)} of the final ones right.`}
        </p>
      ) : null}
      {tried.kept_tests.length > 0 ? (
        <div className="flex flex-col">
          <h3 className="m-0 border-t border-line pt-3 text-secondary font-control text-ink-soft">Kept tests</h3>
          <ul className="m-0 flex list-none flex-col p-0">
            {tried.kept_tests.map((test) => (
              <li
                key={test.test}
                className="flex items-center justify-between gap-3 border-t border-line py-3 first:border-t-0"
              >
                <span className="min-w-0 break-words">
                  <span className="mr-2 text-muted tabular-nums">#{test.test}</span>
                  <span className="text-ink">{titleOf(test.test)}</span>
                </span>
                <Chip tone={test.result === 'passed' ? 'ok' : 'rework'}>
                  {test.result === 'passed' ? 'Passed' : 'Failed'}
                </Chip>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </SectionCard>
  );
}

/** How many of an item's tries passed (were graded fixed), and how many there are. */
function passedOf(item: TriedItem): { passed: number; total: number } {
  return { passed: item.tries.filter((t) => t.verdict === 'fixed').length, total: item.tries.length };
}

/** The cell's fill: all passed is ok, one or none of three is sent back, else plain. */
function cellTone(passed: number, total: number): string {
  if (total > 0 && passed === total) return 'bg-ok-tint text-ok-ink';
  if (passed * 2 < total) return 'bg-rework-tint text-rework-ink';
  return 'bg-hover text-ink';
}

/** A model proposal's tries: each item with how it went on the line and how many of its three tries passed. */
export function ModelTries({
  tried,
  model,
  action,
}: {
  tried: ProposalTried;
  model: string | undefined;
  action?: ReactNode;
}) {
  return (
    <SectionCard
      title={
        <span className="inline-flex items-center gap-1">
          Tried on past items
          <InfoTip label="How model tries work">
            Each item is rerun three times with the new model, then graded against what the line did.
          </InfoTip>
        </span>
      }
      aside={
        <span className="flex flex-wrap items-center justify-end gap-x-3 gap-y-1">
          <span className="text-secondary text-muted">{triedAside(tried)}</span>
          {action}
        </span>
      }
    >
      <table aria-label="Tries per item" className="w-full table-fixed border-collapse text-left">
        <thead>
          <tr className="text-secondary text-muted">
            <th scope="col" className="pb-2 font-normal">
              Item
            </th>
            <th scope="col" className="w-24 pb-2 font-normal md:w-32">
              On the line
            </th>
            <th scope="col" className="w-24 pb-2 text-center font-normal break-words md:w-36">
              {model ?? 'Tries'}
            </th>
          </tr>
        </thead>
        <tbody>
          {tried.items.map((item) => {
            const { passed, total } = passedOf(item);
            const first = passedFirstTime(item);
            return (
              <tr key={item.item_ref} className="min-h-11 border-t border-line">
                <th scope="row" className="min-w-0 py-2 pr-3 font-normal break-words" title={item.line_result}>
                  <span className="mr-2 text-muted tabular-nums">{itemNumber(item.item_ref)}</span>
                  {item.item_title}
                </th>
                <td className="py-2">
                  <Chip tone={first ? 'ok' : 'rework'}>{first ? 'Passed' : 'Sent back'}</Chip>
                </td>
                <td className="py-2">
                  <span
                    className={`block rounded-control px-3 py-2 text-center font-control tabular-nums transition-colors duration-(--duration-quick) ${cellTone(passed, total)}`}
                  >
                    {passed} of {total}
                    <span className="sr-only">
                      {`: ${item.tries.map((t) => `try ${t.attempt} ${t.verdict ? verdictLabels[t.verdict].toLowerCase() : t.state}`).join(', ')}`}
                    </span>
                  </span>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </SectionCard>
  );
}

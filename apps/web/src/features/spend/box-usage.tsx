// Box usage: the CPU and peak memory the window's runs used, by agent or by station, per box size, so
// people can see which ones use what they pay for and whether the box fits. Rows keep the API's order
// (largest CPU P90 first). From md up it is a table; below md each row stacks, as Budgets does.
import { useState } from 'react';
import type { AgentUsage, OrgSpend, StationUsage } from '../../api/types';
import { formatCpuSeconds, formatMemory } from '../../shared/format';
import { EmptyState } from '../../ui/empty-state';
import { InfoTip } from '../../ui/info-tip';
import { SegmentedControl } from '../../ui/segmented-control';

type Grouping = 'agents' | 'stations';
type UsageRow = AgentUsage | StationUsage;

const tip =
  'Median is the middle run; P90 is the run that 9 in 10 stay under. CPU runs from prepare to the run’s end. Runs the box couldn’t measure are left out.';

const sizeLabel = { small: 'Small', medium: 'Medium', large: 'Large' } as const;

/** The section as muted blocks, in the slot of what replaces it. */
export function BoxUsagePlaceholder() {
  return <div className="h-56 rounded-card bg-hover" />;
}

export function BoxUsage({ spend, factoryNames }: { spend: OrgSpend; factoryNames: ReadonlyMap<string, string> }) {
  const [grouping, setGrouping] = useState<Grouping>('agents');
  const rows: UsageRow[] = grouping === 'agents' ? spend.usage.by_agent : spend.usage.by_station;
  const many = new Set([...spend.usage.by_agent, ...spend.usage.by_station].map((r) => r.factory_id)).size > 1;
  const nameOf = (r: UsageRow) => ('agent' in r ? r.agent : r.station);
  const head = grouping === 'agents' ? 'Agent' : 'Station';

  return (
    <section aria-labelledby="bu-h" className="flex min-w-0 flex-col rounded-card border border-line bg-card p-4">
      <div className="mb-3 flex flex-wrap items-center gap-x-2 gap-y-2">
        <h2 id="bu-h" className="m-0 text-heading text-ink">
          Box usage
        </h2>
        <InfoTip about="Box usage">{tip}</InfoTip>
        <div className="flex max-w-full md:ml-auto">
          <SegmentedControl
            label="Group box usage by"
            options={[
              { value: 'agents', label: 'Agents' },
              { value: 'stations', label: 'Stations' },
            ]}
            value={grouping}
            onChange={setGrouping}
            className="max-w-full"
          />
        </div>
      </div>
      {rows.length === 0 ? (
        <EmptyState title="No box usage in this window">
          Runs show here once a box reports its CPU and memory.
        </EmptyState>
      ) : (
        <table className="m-0 w-full border-collapse text-body max-md:block">
          <thead className="max-md:sr-only">
            <tr className="text-left text-meta text-muted [&>th]:pb-2 [&>th]:font-medium">
              <th scope="col">{head}</th>
              <th scope="col">Box</th>
              <th scope="col" className="text-right">
                Runs
              </th>
              <th scope="col" className="text-right">
                CPU median
              </th>
              <th scope="col" className="text-right">
                CPU P90
              </th>
              <th scope="col" className="text-right">
                Memory median
              </th>
              <th scope="col" className="text-right">
                Memory P90
              </th>
            </tr>
          </thead>
          <tbody className="max-md:block">
            {rows.map((r) => (
              <tr
                key={`${r.factory_id}/${nameOf(r)}/${r.box_size ?? ''}`}
                className="border-t border-line max-md:grid max-md:grid-cols-2 max-md:gap-x-3 max-md:gap-y-1 max-md:py-3"
              >
                <td className="min-w-0 py-3 pr-3 max-md:col-span-2 max-md:p-0">
                  <b className="font-semibold break-words text-ink">{nameOf(r)}</b>
                  {many ? (
                    <span className="block text-meta text-muted">{factoryNames.get(r.factory_id) ?? r.factory_id}</span>
                  ) : null}
                </td>
                <Cell label="Box" left>
                  {r.box_size ? sizeLabel[r.box_size] : 'Unknown'}
                </Cell>
                <Cell label="Runs">{r.runs}</Cell>
                <Cell label="CPU median">{formatCpuSeconds(r.cpu_seconds_p50)}</Cell>
                <Cell label="CPU P90">{formatCpuSeconds(r.cpu_seconds_p90)}</Cell>
                <Cell label="Memory median">{formatMemory(r.peak_memory_bytes_p50)}</Cell>
                <Cell label="Memory P90">{formatMemory(r.peak_memory_bytes_p90)}</Cell>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

/** One figure. Below md the column's name shows before it (the header row is hidden there). */
function Cell({ label, left, children }: { label: string; left?: boolean; children: React.ReactNode }) {
  return (
    <td
      className={`py-3 pl-3 tabular-nums whitespace-nowrap max-md:flex max-md:justify-between max-md:p-0 ${left ? 'text-left md:pl-0' : 'text-right'}`}
    >
      <span className="text-meta text-muted md:hidden" aria-hidden="true">
        {label}
      </span>
      {children}
    </td>
  );
}

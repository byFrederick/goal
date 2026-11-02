// Spend and billing (canvas "Spend and billing", organization/spend.html): what the organization's agent
// runs cost over a window, where the money went (by day, factory, agent and model), each factory's
// budget against today and the week, and who pays for the models. The window is in the URL
// (?window=week|month|90d) and the figures come from getOrgSpend. Export CSV downloads the same window as
// CSV (exportOrgSpend, ADR 0020 decision 3).
import { useMutation, useQuery } from '@tanstack/react-query';
import { useNavigate, useSearch } from '@tanstack/react-router';
import { Download, TriangleAlert } from 'lucide-react';
import { exportFailure, saveCsv } from '../../api/download';
import { spendQuery } from '../../api/queries';
import { exportOrgSpend } from '../../api/spend';
import type { OrgSpend } from '../../api/types';
import { TopBarSlot } from '../../app/top-bar';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { EmptyState } from '../../ui/empty-state';
import { PageHeader } from '../../ui/page-header';
import { SegmentedControl } from '../../ui/segmented-control';
import { Skeleton } from '../../ui/skeleton';
import { Spinner } from '../../ui/spinner';
import { BarList, type SpendRow } from './bar-list';
import { Budgets } from './budgets';
import { SpendDays } from './days-chart';
import { Payers } from './payers';
import { spendSearch, spendWindows } from './search';
import { SpendStats } from './spend-stats';
import { factoryLines, windowLabel } from './view';

export function SpendPage() {
  const org = useOrg();
  const window = spendSearch(useSearch({ strict: false })).window ?? 'week';
  const navigate = useNavigate();
  const query = useQuery(spendQuery(org.id, window));
  const exportSpend = useMutation({
    mutationFn: () => exportOrgSpend(org.id, window),
    onSuccess: saveCsv,
  });
  const exporting = exportSpend.isPending;

  const windowControl = (
    <SegmentedControl
      label="Time window"
      options={spendWindows.map((w) => ({ value: w, label: windowLabel[w] }))}
      value={window}
      onChange={(w) => void navigate({ to: '/o/$org/spend', params: { org: org.slug }, search: { window: w } })}
      className="max-w-full"
    />
  );
  const exportCsv = (
    <Button
      variant="secondary"
      size="sm"
      aria-busy={exporting || undefined}
      onClick={() => {
        if (!exporting) exportSpend.mutate();
      }}
      className="aria-busy:cursor-progress"
    >
      {exporting ? <Spinner tone="dark" /> : <Download size={15} strokeWidth={1.6} aria-hidden="true" />}
      Export CSV
    </Button>
  );

  return (
    <div className="flex min-w-0 flex-col gap-4 p-4 md:p-7">
      <TopBarSlot
        actions={
          <>
            {windowControl}
            {exportCsv}
          </>
        }
      />
      <PageHeader title="Spend and billing" />
      {exportSpend.isError ? (
        <div role="alert">
          <Callout tone="rework">{exportFailure(exportSpend.error, 'spend')}</Callout>
        </div>
      ) : null}
      {query.isPending ? (
        <div aria-busy="true">
          <span role="status" className="sr-only">
            Loading spend and billing
          </span>
          <Skeleton ready={false} placeholder={<SpendPlaceholder />}>
            {null}
          </Skeleton>
        </div>
      ) : query.isError ? (
        <div
          className="flex max-w-xl flex-col items-start gap-3 rounded-card border border-line bg-card p-5"
          role="alert"
        >
          <Callout tone="rework">Couldn’t load spend and billing. Check your connection and try again.</Callout>
          <Button variant="white" onClick={() => void query.refetch()}>
            Try again
          </Button>
        </div>
      ) : isEmpty(query.data) ? (
        <EmptyState icon={<TriangleAlert size={20} strokeWidth={1.5} />} title="No spend in this window">
          Costs appear here once a factory's agents run.
        </EmptyState>
      ) : (
        <Ready spend={query.data} org={org.slug} />
      )}
    </div>
  );
}

/** The stat strip and the chart as muted blocks, in the slot of what replaces them. */
function SpendPlaceholder() {
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap gap-4">
        {['a', 'b', 'c', 'd'].map((k) => (
          <div key={k} className="h-16 min-w-36 flex-1 rounded-control bg-hover" />
        ))}
      </div>
      <div className="h-72 rounded-card bg-hover" />
    </div>
  );
}

/** No money spent in the window and no factory with a budget: the screen says so once, not as zeros. */
function isEmpty(spend: OrgSpend): boolean {
  return spend.budgets.length === 0 && spend.days.every((d) => d.usd === 0);
}

function Ready({ spend, org }: { spend: OrgSpend; org: string }) {
  const lines = factoryLines(spend);
  const nameOf = new Map(lines.map((l) => [l.id, l.name]));
  const agents: SpendRow[] = spend.by_agent.map((a) => ({
    key: `${a.factory_id}/${a.agent}`,
    label: a.agent,
    usd: a.usd,
    detail: nameOf.get(a.factory_id),
  }));
  const models: SpendRow[] = spend.by_model.map((m) => ({ key: m.model, label: m.model, usd: m.usd }));
  const notSplit = spend.complete ? 'Nothing spent in this window.' : 'Not split by factory, so not shown.';

  return (
    <>
      <SpendStats spend={spend} />
      <SpendDays spend={spend} />
      <div className="grid gap-4 lg:grid-cols-2">
        <BarList labelledBy="ag-h" title="By agent" rows={agents} empty={notSplit} />
        <BarList labelledBy="md-h" title="By model" rows={models} empty={notSplit} />
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        <Budgets budgets={spend.budgets} />
        <Payers budgets={spend.budgets} org={org} total={spend.budgets.length} />
      </div>
    </>
  );
}

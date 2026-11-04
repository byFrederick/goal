// Jonah's header (MASTER.md "Jonah"): the name and the intro of the tab the page is on, then the tabs
// Overview, Proposals and Tests, each with its own URL. The tab the page is on is current, and Proposals and Tests
// show their counts once they are known. A page that sets its own title uses JonahTabs, the tab bar alone.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { keptTestsQuery, proposalsQuery } from '../../api/queries';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { TabNav, tabNavUnderlineItem } from '../../ui/tab-nav';

export type JonahTab = 'overview' | 'proposals' | 'tests';

const tabLabels: Record<JonahTab, string> = {
  overview: 'Overview',
  proposals: 'Proposals',
  tests: 'Tests',
};

/** The intro line under the name, per tab, in the factory's own name (the pictures' words). */
function introOf(tab: JonahTab, factory: string): string {
  if (tab === 'tests') return 'Past items Goal reruns when a change reaches their station.';
  return `Proposes changes to ${factory}'s definition. Nothing changes until you approve.`;
}

export function JonahHeader({ tab }: { tab: JonahTab }) {
  const factory = useFactory();
  return (
    <div className="flex min-w-0 flex-col gap-4">
      <header className="flex flex-wrap items-center gap-3">
        <div className="flex min-w-0 flex-col gap-1">
          <h1 className="m-0 text-page-title tracking-display">Jonah</h1>
          <p className="m-0 text-body text-ink-soft">{introOf(tab, factory.title ?? factory.name)}</p>
        </div>
      </header>
      <JonahTabs tab={tab} />
    </div>
  );
}

/** The counts of the tabs: all proposals (the first page, with a plus when more follow) and the kept tests. */
function useTabCounts(): Partial<Record<JonahTab, string>> {
  const org = useOrg();
  const factory = useFactory();
  const proposals = useQuery(proposalsQuery(org.id, factory.id));
  const tests = useQuery(keptTestsQuery(org.id, factory.id));
  return {
    ...(proposals.data
      ? { proposals: `${proposals.data.proposals.length}${proposals.data.next === undefined ? '' : '+'}` }
      : {}),
    ...(tests.data ? { tests: String(tests.data.tests.length) } : {}),
  };
}

/** The Jonah tab bar: Overview, Proposals and Tests, with the tab the page is on current. */
export function JonahTabs({ tab }: { tab: JonahTab }) {
  const params = useFactoryParams();
  const counts = useTabCounts();
  const item = (t: JonahTab) => ({
    'aria-current': t === tab ? ('page' as const) : undefined,
    className: tabNavUnderlineItem,
    activeOptions: { exact: true },
    params,
  });
  const label = (t: JonahTab) => (
    <>
      {tabLabels[t]}
      {counts[t] === undefined ? null : (
        <span className="text-meta font-normal text-muted tabular-nums">{counts[t]}</span>
      )}
    </>
  );
  return (
    <TabNav label="Jonah" variant="underline">
      <Link to="/o/$org/f/$factory/jonah" {...item('overview')}>
        {label('overview')}
      </Link>
      <Link to="/o/$org/f/$factory/jonah/proposals" {...item('proposals')}>
        {label('proposals')}
      </Link>
      <Link to="/o/$org/f/$factory/jonah/tests" {...item('tests')}>
        {label('tests')}
      </Link>
    </TabNav>
  );
}

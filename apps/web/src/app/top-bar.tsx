// The top bar (MASTER.md "Top bar"): from 768 px, a 56 px row above main. On the left, a breadcrumb:
// on a factory page the factory's badge and name, then the page; on an organization page, the page.
// On the right, the page's actions, the primary one last. Below 768 px the phone's top bar takes its
// place (phone-chrome.tsx): the same page name and actions, without the crumbs.
//
// The page's names come from screens.ts, keyed by the route the URL matched. A page sets its last
// crumb (a detail's name, once its query resolves) and its actions with <TopBarSlot>; until it does,
// the bar shows the page's name alone. ShellFrame holds the state and passes it here.

import { useQuery } from '@tanstack/react-query';
import { Link, useMatches } from '@tanstack/react-router';
import { ChevronRight } from 'lucide-react';
import { createContext, type Dispatch, type ReactNode, type SetStateAction, useContext, useLayoutEffect } from 'react';
import { factoriesQuery } from '../api/queries';
import { factoryTitle, useFactory } from '../shared/factory';
import { factoryMark } from '../shared/marks';
import { useOrg } from '../shared/org';
import { FactoryBadge } from './factory-badge';
import {
  apiKeysScreen,
  connectionsScreen,
  credentialsScreen,
  homeScreen,
  knowledgeScreen,
  membersScreen,
  newFactoryScreen,
  type OrgPath,
  runsScreen,
  settingsScreen,
  sidebarFactoryPages,
} from './screens';

/**
 * What a page sets: its last crumb (undefined shows the page's name) and its actions. `back` is the
 * phone's Back link, shown after the Goal mark; the desktop bar has no use for it and leaves it out.
 */
export interface TopBarState {
  crumb?: string | undefined;
  /** A crumb between the page and its last crumb, such as Bake-offs above one bake-off. Factory pages only. */
  parent?: { label: string; to: '/o/$org/f/$factory/jonah/bake-offs' } | undefined;
  actions?: ReactNode | undefined;
  back?: ReactNode | undefined;
}

type SetTopBar = Dispatch<SetStateAction<TopBarState | null>>;

/** ShellFrame's setter, so a page's <TopBarSlot> reaches the bar. Null outside a ShellFrame. */
export const TopBarSetContext = createContext<SetTopBar | null>(null);

/**
 * Sets the top bar's last crumb and actions while the page is mounted, and clears them when it
 * unmounts. Renders nothing itself.
 */
export function TopBarSlot({ crumb, parent, actions, back }: TopBarState) {
  const set = useContext(TopBarSetContext);
  useLayoutEffect(() => {
    set?.({ crumb, parent, actions, back });
    return () => set?.(null);
  }, [set, crumb, parent, actions, back]);
  return null;
}

/** The factory page each factory route shows, by route id (the id a route match carries). */
const factoryRoutePages: Record<string, string> = {
  '/o/$org/f/$factory/': 'floor',
  '/o/$org/f/$factory/board': 'board',
  '/o/$org/f/$factory/items/': 'items',
  '/o/$org/f/$factory/items/$ref': 'items',
  '/o/$org/f/$factory/agents/': 'agents',
  '/o/$org/f/$factory/agents/$agent': 'agents',
  '/o/$org/f/$factory/definition/': 'definition',
  '/o/$org/f/$factory/definition/$section': 'definition',
  '/o/$org/f/$factory/intake/$trigger': 'intake',
  '/o/$org/f/$factory/settings': 'settings',
  '/o/$org/f/$factory/jonah/evals': 'jonah',
  '/o/$org/f/$factory/jonah/judges/': 'jonah',
  '/o/$org/f/$factory/jonah/judges/$judge': 'jonah',
  '/o/$org/f/$factory/jonah/proposals/$number': 'jonah',
  '/o/$org/f/$factory/jonah/bake-offs/': 'jonah',
  '/o/$org/f/$factory/jonah/bake-offs/$bakeOff': 'jonah',
};

/** The organization page each organization route shows, by route id. */
const orgRoutePages: Record<string, { label: string; to?: OrgPath }> = {
  '/o/$org/_org/': homeScreen,
  '/account': { label: 'Your account' },
  '/o/$org/_org/api-keys': apiKeysScreen,
  '/o/$org/_org/members': membersScreen,
  '/o/$org/_org/runs': runsScreen,
  '/o/$org/_org/settings': settingsScreen,
  '/o/$org/_org/connections/': connectionsScreen,
  '/o/$org/_org/connections/$connection': connectionsScreen,
  '/o/$org/_org/connections/new': connectionsScreen,
  '/o/$org/_org/credentials/': credentialsScreen,
  '/o/$org/_org/credentials/new': credentialsScreen,
  '/o/$org/_org/knowledge/': knowledgeScreen,
  '/o/$org/_org/knowledge/$source': knowledgeScreen,
  '/o/$org/_org/knowledge/new': knowledgeScreen,
  '/o/$org/new/$step': { label: newFactoryScreen.label },
};

const crumbLink = 'text-ink-soft no-underline hover:text-ink';

/** The page's name the matched route shows (screens.ts), or undefined for a route the bar does not name. */
export function usePageLabel(): string | undefined {
  return useMatches({ select: (matches) => pageLabel(matches.at(-1)?.routeId ?? '') });
}

function pageLabel(routeId: string): string | undefined {
  if (routeId.startsWith('/o/$org/f/$factory/')) {
    return sidebarFactoryPages.find((p) => p.id === factoryRoutePages[routeId])?.label;
  }
  return orgRoutePages[routeId]?.label;
}

/** The bar, from 768 px. Phones use phone-chrome.tsx's top bar instead (ShellFrame picks one). */
export function TopBar({ state }: { state: TopBarState | null }) {
  const routeId = useMatches({ select: (matches) => matches.at(-1)?.routeId ?? '' });
  const inFactory = routeId.startsWith('/o/$org/f/$factory/');
  return (
    <header className="hidden h-14 flex-none items-center gap-2 border-b border-rule bg-shell px-7 md:flex">
      {inFactory ? (
        <FactoryCrumbs routeId={routeId} crumb={state?.crumb} parent={state?.parent} />
      ) : (
        <OrgCrumbs routeId={routeId} crumb={state?.crumb} />
      )}
      {state?.actions ? <div className="ml-auto flex items-center gap-2">{state.actions}</div> : null}
    </header>
  );
}

function FactoryCrumbs({
  routeId,
  crumb,
  parent,
}: {
  routeId: string;
  crumb: string | undefined;
  parent: TopBarState['parent'];
}) {
  const org = useOrg();
  const factory = useFactory();
  const { data: factories = [] } = useQuery(factoriesQuery(org.id));
  const page = sidebarFactoryPages.find((p) => p.id === factoryRoutePages[routeId]);
  if (!page) return null;
  const params = { org: org.slug, factory: factory.name };
  const mark = factoryMark(
    factory,
    factories.filter((f) => !f.archived_at),
  );
  const items: Crumb[] = [
    {
      key: 'factory',
      node: (
        <>
          <FactoryBadge mark={mark} className="size-5" />
          <Link to="/o/$org/f/$factory" params={params} className={crumbLink}>
            {factoryTitle(factory)}
          </Link>
        </>
      ),
    },
  ];
  if (crumb === undefined) {
    items.push({ key: 'page', node: <Current>{page.label}</Current> });
  } else {
    items.push({
      key: 'page',
      node: page.available ? (
        <Link to={page.to} params={params} className={crumbLink}>
          {page.label}
        </Link>
      ) : (
        page.label
      ),
    });
    if (parent) {
      items.push({
        key: 'parent',
        node: (
          <Link to={parent.to} params={params} className={crumbLink}>
            {parent.label}
          </Link>
        ),
      });
    }
    items.push({ key: 'crumb', node: <Current>{crumb}</Current> });
  }
  return <Trail items={items} />;
}

function OrgCrumbs({ routeId, crumb }: { routeId: string; crumb: string | undefined }) {
  const org = useOrg();
  const screen: { label: string; to?: OrgPath } | undefined = orgRoutePages[routeId];
  if (!screen) return null;
  const page = screen.to ? (
    <Link to={screen.to} params={{ org: org.slug }} className={crumbLink}>
      {screen.label}
    </Link>
  ) : (
    screen.label
  );
  const items: Crumb[] =
    crumb === undefined
      ? [{ key: 'page', node: <Current>{screen.label}</Current> }]
      : [
          { key: 'page', node: page },
          { key: 'crumb', node: <Current>{crumb}</Current> },
        ];
  return <Trail items={items} />;
}

interface Crumb {
  key: string;
  node: ReactNode;
}

/** The breadcrumb: its crumbs in order, a chevron between each pair. */
function Trail({ items }: { items: Crumb[] }) {
  return (
    <nav aria-label="Breadcrumb" className="min-w-0">
      <ol className="m-0 flex min-w-0 list-none items-center gap-2 p-0 font-control text-body text-ink-soft">
        {items.map((item, i) => (
          <li key={item.key} className="flex min-w-0 items-center gap-2">
            {i > 0 ? (
              <ChevronRight size={12} strokeWidth={1.5} aria-hidden="true" className="flex-none text-muted" />
            ) : null}
            {item.node}
          </li>
        ))}
      </ol>
    </nav>
  );
}

/** The last crumb: the page the person is on, in ink. */
function Current({ children }: { children: ReactNode }) {
  return (
    <span aria-current="page" className="font-control text-ink">
      {children}
    </span>
  );
}

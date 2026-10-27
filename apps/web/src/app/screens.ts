// Every screen the shell links to, in one registry that drives the sidebar, the tab bar and the More
// sheet (C §10.5). A screen a later phase builds is listed with `available: false` and shows as
// "Later" (the phone) or "Not available yet" (the sidebar); the PR that adds its route flips it to
// `available: true` with its `to`. Every organization screen exists.
import {
  Activity,
  BookOpen,
  Code,
  Factory,
  House,
  Inbox,
  Kanban,
  KeyRound,
  LayoutGrid,
  type LucideIcon,
  Plug,
  Plus,
  SearchCheck,
  Settings,
  SquareTerminal,
  TextAlignStart,
  Users,
} from 'lucide-react';

interface Screen {
  id: string;
  label: string;
  icon: LucideIcon;
}

/** Paths of a factory's screens; their params are the organization's slug and the factory's name. */
export type FactoryPath =
  | '/o/$org/f/$factory'
  | '/o/$org/f/$factory/board'
  | '/o/$org/f/$factory/items'
  | '/o/$org/f/$factory/definition'
  | '/o/$org/f/$factory/settings';
/** Paths of the organization's screens; their only param is its slug. */
export type OrgPath =
  | '/o/$org'
  | '/o/$org/members'
  | '/o/$org/settings'
  | '/o/$org/api-keys'
  | '/o/$org/connections'
  | '/o/$org/credentials'
  | '/o/$org/knowledge';

type Availability<Path> = { available: true; to: Path; exact?: boolean } | { available: false };

/**
 * A factory screen: one of its own pages, or (`filters`) an organization page filtered to the
 * factory (?factory=<name>), as the factory's Connections and Knowledge are.
 */
export type FactoryScreen = Screen & { group: 'work' | 'build' | 'learn' } & (
    | Availability<FactoryPath>
    | { available: true; filters: '/o/$org/connections' | '/o/$org/knowledge' }
  );
/**
 * An organization screen; one with `permissions` is listed only for people who hold at least one
 * of them (Org.can).
 */
export type OrgScreen = Screen & { to: OrgPath; exact?: boolean; permissions?: readonly string[] };

/** The factory sidebar, top to bottom: Work, Build, Learn. */
export const factoryScreens: readonly FactoryScreen[] = [
  {
    id: 'floor',
    label: 'Floor',
    icon: LayoutGrid,
    group: 'work',
    available: true,
    to: '/o/$org/f/$factory',
    exact: true,
  },
  { id: 'board', label: 'Board', icon: Kanban, group: 'work', available: true, to: '/o/$org/f/$factory/board' },
  { id: 'items', label: 'Items', icon: TextAlignStart, group: 'work', available: true, to: '/o/$org/f/$factory/items' },
  // D: /definition/:section (current on E's /agents/* too, once it exists).
  {
    id: 'definition',
    label: 'Definition',
    icon: Code,
    group: 'build',
    available: true,
    to: '/o/$org/f/$factory/definition',
  },
  // The factory's own settings: Pause or Resume and the Danger zone (archive, restore, delete).
  {
    id: 'settings',
    label: 'Settings',
    icon: Settings,
    group: 'build',
    available: true,
    to: '/o/$org/f/$factory/settings',
  },
  // The organization's Connections, filtered to the factory (spec 4 §10).
  {
    id: 'connections',
    label: 'Connections',
    icon: Plug,
    group: 'build',
    available: true,
    filters: '/o/$org/connections',
  },
  // The organization's Knowledge, filtered to the factory (spec 4 §10). Later: /jonah (phase 5).
  {
    id: 'knowledge',
    label: 'Knowledge',
    icon: BookOpen,
    group: 'learn',
    available: true,
    filters: '/o/$org/knowledge',
  },
  { id: 'jonah', label: 'Jonah', icon: SearchCheck, group: 'learn', available: false },
];

export const factoryGroups = [
  { id: 'work', label: 'Work' },
  { id: 'build', label: 'Build' },
  { id: 'learn', label: 'Learn' },
] as const;

export const factoriesScreen: OrgScreen = {
  id: 'factories',
  label: 'Factories',
  icon: Factory,
  to: '/o/$org',
  exact: true,
};
/** Members and roles (spec 3, PR 3-11): every member reaches it; owners and admins change people. */
export const membersScreen: OrgScreen = {
  id: 'members',
  label: 'Members and roles',
  icon: Users,
  to: '/o/$org/members',
};

/**
 * The organization's API keys (spec 3 §10.2 "Api-Keys") for owners and admins, and everyone's own
 * personal tokens (3-12b) on the same page, so every member reaches it.
 */
export const apiKeysScreen: OrgScreen = {
  id: 'api-keys',
  label: 'API keys',
  icon: SquareTerminal,
  to: '/o/$org/api-keys',
  permissions: ['api_key:read', 'personal_token:read'],
};
/** The organization's connections (spec 4 §10): every member reads them; owners and admins change them. */
export const connectionsScreen: OrgScreen = {
  id: 'connections',
  label: 'Connections',
  icon: Plug,
  to: '/o/$org/connections',
  permissions: ['connection:read'],
};
/** The organization's credentials (ADR 0017): every member reads their names; owners and admins change them. */
export const credentialsScreen: OrgScreen = {
  id: 'credentials',
  label: 'Credentials',
  icon: KeyRound,
  to: '/o/$org/credentials',
  permissions: ['credential:read'],
};
/** The organization's knowledge base (spec 4 §10): every member reads and searches it; owners and admins change it. */
export const knowledgeScreen: OrgScreen = {
  id: 'knowledge',
  label: 'Knowledge',
  icon: BookOpen,
  to: '/o/$org/knowledge',
  permissions: ['knowledge:read'],
};
export const settingsScreen: OrgScreen = {
  id: 'settings',
  label: 'Settings',
  icon: Settings,
  to: '/o/$org/settings',
};

/**
 * A sidebar entry (sidebar.tsx). With `available: false` it has no page yet: it is listed and disabled,
 * "Not available yet" (ADR 0016, decision 4), and the item that builds its page flips it.
 */
type Listed<Path extends string> = { id: string; label: string; icon?: LucideIcon; permissions?: readonly string[] } & (
  | { available: true; to: Path; exact?: boolean }
  | { available: false }
);

/** The organization's Home: the factories list (the sidebar's first entry, the top bar's page name). */
export const homeScreen: OrgScreen = { id: 'home', label: 'Home', icon: House, to: '/o/$org', exact: true };

/** The sidebar's organization-wide entries, top to bottom. */
export const sidebarOrgScreens: readonly Listed<OrgPath>[] = [
  { ...homeScreen, available: true },
  { id: 'needs-you', label: 'Needs you', icon: Inbox, available: false },
  { id: 'runs', label: 'Runs', icon: Activity, available: false },
  { ...connectionsScreen, available: true },
  { ...credentialsScreen, available: true },
  { ...knowledgeScreen, available: true },
];

/** The pages the sidebar lists under the open factory, top to bottom. */
export const sidebarFactoryPages: readonly Listed<FactoryPath>[] = [
  { id: 'floor', label: 'Floor', available: true, to: '/o/$org/f/$factory', exact: true },
  { id: 'board', label: 'Board', available: true, to: '/o/$org/f/$factory/board' },
  { id: 'items', label: 'Items', available: true, to: '/o/$org/f/$factory/items' },
  { id: 'agents', label: 'Agents', available: false },
  { id: 'intake', label: 'Intake', available: false },
  { id: 'jonah', label: 'Jonah', available: false },
  { id: 'definition', label: 'Definition', available: true, to: '/o/$org/f/$factory/definition' },
  { id: 'settings', label: 'Settings', available: true, to: '/o/$org/f/$factory/settings' },
];

/** The screens of `screens` a person holding `can` sees; none of the gated ones while `can` loads. */
export function screensFor<S extends { permissions?: readonly string[] }>(
  screens: readonly S[],
  can: readonly string[] | undefined,
): S[] {
  return screens.filter((s) => !s.permissions || s.permissions.some((p) => can?.includes(p) ?? false));
}

/** New factory: D's wizard, /o/:org/new/:step, from Start (the sidebar's + and the factory picker). */
export const newFactoryScreen: Screen & { available: true; to: '/o/$org/new/$step' } = {
  id: 'new-factory',
  label: 'New factory',
  icon: Plus,
  available: true,
  to: '/o/$org/new/$step',
};

/** Shown beside a screen a later phase builds. */
export const laterText = 'Later';

// Every screen the shell links to, in one registry that drives the sidebars, the rail, the tab bar
// and the More sheet (C §10.5). A screen a later phase builds is listed with `available: false`
// and shows as "Later"; the PR that adds its route flips it to `available: true` with its `to`.
import {
  BookOpen,
  Code,
  Factory,
  Kanban,
  KeyRound,
  Layers,
  LayoutGrid,
  type LucideIcon,
  Plug,
  Plus,
  SearchCheck,
  Settings,
  TextAlignStart,
} from 'lucide-react';

interface Screen {
  id: string;
  label: string;
  icon: LucideIcon;
}

/** Paths of a factory's screens; their params are the organization's slug and the factory's name. */
export type FactoryPath = '/o/$org/f/$factory' | '/o/$org/f/$factory/board' | '/o/$org/f/$factory/items';
/** Paths of the organization's screens; their only param is its slug. */
export type OrgPath = '/o/$org' | '/o/$org/settings';

type Availability<Path> = { available: true; to: Path; exact?: boolean } | { available: false };

export type FactoryScreen = Screen & { group: 'work' | 'build' | 'learn' } & Availability<FactoryPath>;
export type OrgScreen = Screen & Availability<OrgPath>;

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
  // D: /definition/:section (current on /agents/* too).
  { id: 'definition', label: 'Definition', icon: Code, group: 'build', available: false },
  // Phase 3.2: /connections.
  { id: 'connections', label: 'Connections', icon: Plug, group: 'build', available: false },
  // Later: /knowledge, /jonah.
  { id: 'knowledge', label: 'Knowledge', icon: BookOpen, group: 'learn', available: false },
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
  available: true,
  to: '/o/$org',
  exact: true,
};
// Phase 3.2: /o/:org/catalog.
export const catalogScreen: OrgScreen = { id: 'catalog', label: 'Connector catalog', icon: Layers, available: false };
// Phase 3.1: /o/:org/credentials.
export const credentialsScreen: OrgScreen = {
  id: 'credentials',
  label: 'Credentials',
  icon: KeyRound,
  available: false,
};
export const settingsScreen: OrgScreen = {
  id: 'settings',
  label: 'Settings',
  icon: Settings,
  available: true,
  to: '/o/$org/settings',
};

/** The organization sidebar, top to bottom. */
export const orgScreens: readonly OrgScreen[] = [factoriesScreen, catalogScreen, credentialsScreen, settingsScreen];

/** The rail's dashed square. D: /o/:org/new/:step (no sidebar). */
export const newFactoryScreen: Screen & { available: false } = {
  id: 'new-factory',
  label: 'New factory',
  icon: Plus,
  available: false,
};

/** Shown beside a screen a later phase builds. */
export const laterText = 'Later';

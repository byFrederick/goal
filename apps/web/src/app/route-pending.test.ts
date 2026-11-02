import { describe, expect, it } from 'vitest';

// Every route file shows a placeholder while its data loads, so no page is ever blank (MOTION.md, Loading).
// A route sets its own pendingComponent (the page skeleton, or a list or Board one: route-skeletons.tsx).
// A layout only renders an Outlet, and the router's default pending component (the page skeleton) covers
// it. Such a file must be listed here, with its reason, so a new layout is a decision and not an oversight.
// The organization and factory layouts draw the shell's frame instead: they set ShellPending.
const layouts = new Map<string, string>([
  ['__root.tsx', 'the root route: renders the Outlet of every page'],
  ['o.$org.f.$factory.definition.tsx', 'the definition layout: renders an Outlet'],
]);

const shellLayouts = ['o.$org.tsx', 'o.$org._org.tsx', 'o.$org.f.$factory.tsx'];

const ownSkeletons = new Map([
  ['o.$org._org.index.tsx', 'HomeSkeleton'],
  ['o.$org.f.$factory.agents.index.tsx', 'AgentsSkeleton'],
  ['o.$org.f.$factory.agents.$agent.tsx', 'AgentSkeleton'],
  ['o.$org.f.$factory.items.index.tsx', 'ItemsSkeleton'],
  ['o.$org.f.$factory.index.tsx', 'FloorSkeleton'],
  ['o.$org.f.$factory.board.tsx', 'BoardSkeleton'],
  ['o.$org.f.$factory.items.$ref.tsx', 'ItemSkeleton'],
  ['o.$org._org.runs.tsx', 'ListPageSkeleton'],
  ['o.$org.f.$factory.jonah.index.tsx', 'JonahPending'],
]);

const routes = import.meta.glob<string>(['../routes/*.tsx', '!../routes/*.test.tsx'], {
  query: '?raw',
  import: 'default',
  eager: true,
});

const files = Object.keys(routes).map((path) => path.split('/').pop() ?? path);

describe('route pending components', () => {
  it('finds the route files', () => {
    expect(files.length).toBeGreaterThan(40);
  });

  it('keeps the layout list honest: each listed layout exists', () => {
    for (const file of layouts.keys()) {
      expect(files, `${file} is listed as a layout but is not in routes/`).toContain(file);
    }
  });

  it('gives every route a pendingComponent, or lists it as a layout that inherits the default', () => {
    const missing = Object.entries(routes)
      .map(([path, source]) => ({ file: path.split('/').pop() ?? path, source }))
      .filter(({ file, source }) => !layouts.has(file) && !/^\s*pendingComponent:/m.test(source))
      .map(({ file }) => file);

    expect(missing, `these route files set no pendingComponent: ${missing.join(', ')}`).toEqual([]);
  });

  it('draws the shell frame while the organization and factory layouts load', () => {
    for (const file of shellLayouts) {
      const source = routes[`../routes/${file}`] ?? '';
      expect(source, file).toMatch(/pendingComponent:\s*ShellPending/);
    }
  });

  it('gives Home, Floor, Board, Items, an item, Runs, Jonah, Agents and an agent their own skeleton', () => {
    for (const [file, name] of ownSkeletons) {
      const source = routes[`../routes/${file}`] ?? '';
      expect(source, file).toMatch(new RegExp(`pendingComponent:\\s*${name}\\b`));
    }
  });
});

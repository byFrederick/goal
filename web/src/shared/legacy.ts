// Old links: before organizations, factory pages lived at /f/:factory/... (C §10.1). They now
// live at /o/:org/f/:factory/...; the /f/* route finds the factory's organization and redirects.

export interface LegacyTarget {
  /** The factory's name or id, as the old link named it. */
  factory: string;
  /** The rest of the path after the factory ("/items/12"), or "" for its floor. */
  rest: string;
}

/** The factory and the rest of an old /f/ path's splat ("goal/items/12"); null without a factory. */
export function legacyTarget(splat: string): LegacyTarget | null {
  const [factory, ...rest] = splat.split('/').filter(Boolean);
  if (!factory) return null;
  return { factory, rest: rest.map((s) => `/${s}`).join('') };
}

/** The new path of an old link once its organization is known, every segment encoded. */
export function legacyPath(org: string, target: LegacyTarget): string {
  const segments = ['o', org, 'f', target.factory, ...target.rest.split('/').filter(Boolean)];
  return `/${segments.map(encodeURIComponent).join('/')}`;
}

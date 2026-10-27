// The one- or two-letter marks the sidebar, the phone's top row and the factory cards show (C §10.3).

const titleCase = (s: string) => s.charAt(0).toUpperCase() + s.slice(1).toLowerCase();

/**
 * The organization's mark: the short mark it chose, else the initials of the first two words of
 * its name ("Example Labs" → "EL"), else the first two letters title-cased ("Acme" → "Ac").
 */
export function orgMark(org: { name: string; mark: string }): string {
  const chosen = org.mark.trim();
  if (chosen) return chosen;
  const words = org.name.trim().split(/\s+/).filter(Boolean);
  const [first = '', second] = words;
  if (second) return `${first.charAt(0)}${second.charAt(0)}`.toUpperCase();
  return titleCase(first.slice(0, 2));
}

/**
 * A factory's mark: its first letter upper-cased, or its first two ("Su") when a factory that
 * sorts before it by name starts with the same letter. With sre and support: S and Su.
 */
export function factoryMark(factory: { name: string }, siblings: readonly { name: string }[]): string {
  const letter = factory.name.charAt(0).toLowerCase();
  const taken = siblings.some((f) => f.name < factory.name && f.name.charAt(0).toLowerCase() === letter);
  return taken ? titleCase(factory.name.slice(0, 2)) : letter.toUpperCase();
}

// What a person says their factories will do (step 1, "What will your factories do?"). The answer
// stays in this browser, keyed by the new organization's id, for step 3 to suggest templates from.
// Nothing here is sent to goald.
export const orgUses = ['build', 'operations', 'support', 'other'] as const;

export type OrgUse = (typeof orgUses)[number];

const storageKey = (orgId: string) => `goal.org-uses.${orgId}`;

/** The uses saved for an organization; none when storage has no answer or cannot be read. */
export function getOrgUses(orgId: string): OrgUse[] {
  try {
    const raw = localStorage.getItem(storageKey(orgId));
    const saved: unknown = raw ? JSON.parse(raw) : [];
    return Array.isArray(saved) ? orgUses.filter((use) => saved.includes(use)) : [];
  } catch {
    return [];
  }
}

export function setOrgUses(orgId: string, uses: readonly OrgUse[]): void {
  try {
    localStorage.setItem(storageKey(orgId), JSON.stringify(uses));
  } catch {
    // Storage can be unavailable (private windows); step 3 then suggests from nothing.
  }
}

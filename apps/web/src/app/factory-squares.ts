// The organization's factories as the phone's top row shows them: ordered by name, each with its
// mark, what it needs from people and the accessible name that says so. The sidebar lists the same
// factories (sidebar-factories.tsx).
import { useSuspenseQuery } from '@tanstack/react-query';
import { factoriesQuery } from '../api/queries';
import type { Factory, OrgSummary } from '../api/types';
import { factoryMark } from '../shared/marks';
import { type Attention, factorySignal, noAttention, useAttention } from './attention';

export interface FactorySquare {
  factory: Factory;
  mark: string;
  attention: Attention;
  /** The link's accessible name: mark, name and status. */
  label: string;
  /** The tooltip's second line. */
  detail: string;
}

/** The organization's factories that are not archived (an archived one leaves the list and the top row). */
export function useFactorySquares(org: OrgSummary): FactorySquare[] {
  const { data } = useSuspenseQuery(factoriesQuery(org.id));
  const factories = data.filter((f) => !f.archived_at).toSorted((a, b) => a.name.localeCompare(b.name));
  const attention = useAttention(org.id);
  return factories.map((factory) => {
    const mark = factoryMark(factory, factories);
    const a = attention.get(factory.id) ?? noAttention;
    return { factory, mark, attention: a, ...factorySignal(factory, mark, a) };
  });
}

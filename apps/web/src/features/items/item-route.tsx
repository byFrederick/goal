import { useQuery } from '@tanstack/react-query';
import { useParams } from '@tanstack/react-router';
import { partsQuery } from '../../api/queries';
import { itemKey, useFactory } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { AssemblyPage } from './assembly-page';
import { ItemPage } from './item-page';

/** An assembly (an item with parts), in any state, is its own page; every other item keeps the item page. */
export function ItemRoute() {
  const org = useOrg();
  const factory = useFactory();
  const { ref } = useParams({ from: '/o/$org/f/$factory/items/$ref' });
  // A parts error reads as "not an assembly": the item page must open regardless.
  const { data: assembly } = useQuery(partsQuery(org.id, itemKey(factory.name, ref)));
  return (assembly?.total ?? 0) > 0 ? <AssemblyPage /> : <ItemPage />;
}

// The contract's shapes, generated from api/openapi.yaml into @goal/api-client, under the names
// the screens use. Nothing here is written by hand: a shape changes in the contract first.
import type { ItemPage, MergeRequest } from '@goal/api-client';

export type {
  Board,
  BoardStation,
  ClassOfService,
  EventPage,
  Factory,
  FactoryList,
  Item,
  ItemCard,
  ItemEvent,
  ItemState,
  Metrics,
  NewItem,
  NewOrg,
  Org,
  OrgSummary,
  Problem,
  Station,
  StationAction,
  StationKind,
  StreamEvent,
} from '@goal/api-client';

/** One page of a factory's items (`ItemPage` in the contract). */
export type ItemList = ItemPage;

/** How goald merges a pull request (POST /v1/orgs/{org}/items/{item}/merge). */
export type MergeMethod = NonNullable<MergeRequest['method']>;

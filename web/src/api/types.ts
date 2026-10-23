// Hand-written mirror of the goald API v1 shapes (api/openapi.yaml plus the phase 2 board, items,
// metrics, stream, retry and merge endpoints). When @goal/api-client lands, this file re-exports
// its generated types instead; nothing else in the app changes.

export type StationKind = 'queue' | 'agent' | 'human' | 'done';

/** What goald does when a person acts at a human station: merge → POST /v1/items/{item}/merge. */
export type StationAction = 'merge';

export type ItemState = 'queued' | 'ready' | 'in_progress' | 'waiting_human' | 'blocked' | 'done' | 'cancelled';

export type ClassOfService = 'standard' | 'expedite';

export interface Station {
  id: string;
  kind: StationKind;
  action?: StationAction;
}

export interface Factory {
  id: string;
  name: string;
  title?: string;
  description?: string;
  work_types: string[];
  stations: Station[];
}

export interface FactoryList {
  factories: Factory[];
}

/** The summary of an item that lists and boards carry. */
export interface ItemCard {
  id: string;
  /** `<factory name>#<number>`, accepted wherever the API takes an item. */
  ref: string;
  number: number;
  title: string;
  type: string;
  lane?: string | null;
  station: string;
  state: ItemState;
  class_of_service: ClassOfService;
  rework_count: number;
  cost_usd: number;
  created_at: string;
  updated_at: string;
  age_seconds: number;
  /** The agent working the item right now. */
  agent: string | null;
  /** The pull request the item's latest station output names. */
  pr_url: string | null;
}

export interface Item {
  id: string;
  ref: string;
  factory_id: string;
  number: number;
  parent_id?: string;
  type: string;
  lane?: string;
  title: string;
  body: string;
  station: string;
  state: ItemState;
  class_of_service: ClassOfService;
  priority: number;
  rework_count: number;
  cost_usd: number;
  /** Each station's output, by station id. */
  outputs: Record<string, unknown>;
  created_at: string;
  updated_at: string;
  done_at?: string;
}

export interface ItemEvent {
  id: number;
  item_id: string;
  run_id?: string;
  at: string;
  kind: string;
  station?: string;
  actor: string;
  data: Record<string, unknown>;
}

export interface EventPage {
  events: ItemEvent[];
  next_after: number;
}

export interface BoardStation {
  id: string;
  kind: StationKind;
  action?: StationAction;
  agents: string[];
  wip_limit: number | null;
  /** Items queued or ready here (exact, beyond the cards shown). */
  waiting: number;
  in_progress: number;
  /** Up to 500 open cards, newest first; the done station shows the 20 newest. */
  items: ItemCard[];
}

export interface Board {
  factory: { id: string; name: string; title?: string };
  stations: BoardStation[];
  /** The agent station with the most work per slot right now, or null. */
  constraint: string | null;
}

export interface ItemList {
  items: ItemCard[];
  /** Pass as `after` for the next page; null on the last page. */
  next_after: number | null;
}

export interface Metrics {
  window: string;
  throughput: { day: string; done: number }[];
  lead_time_hours: { p50: number; p85: number } | null;
  wip: number;
  /** Share of finished items sent back at least once, 0 to 1. */
  rework_rate: number;
  spend_usd: number;
  items_done: number;
}

export interface NewItem {
  title: string;
  type: string;
  body?: string;
  lane?: string;
  priority?: number;
}

export type MergeMethod = 'squash';

/** The payload of an `item` event on GET /v1/stream. */
export interface StreamEvent {
  item_id: string;
  ref: string;
  factory_id: string;
  kind: string;
  station?: string;
  state: ItemState;
  at: string;
}

/** RFC 9457 problem details. */
export interface Problem {
  title: string;
  status: number;
  detail?: string;
}

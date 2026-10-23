// Typed client for goald's HTTP API. The types in ./schema.ts are generated from
// api/openapi.yaml (`task api:gen`); never edit them by hand.
//
//   const goal = createGoalClient("http://127.0.0.1:8080");
//   const { data, error } = await goal.GET("/v1/factories/{factory}/board", {
//     params: { path: { factory: "goal" } },
//   });
//
// Items are addressed by id or by reference ("goal#2"); path parameters are percent-encoded,
// so a reference can be passed as is.
import createClient, { type ClientOptions } from "openapi-fetch";
import type { components, paths } from "./schema";

export type { components, operations, paths } from "./schema";

type Schemas = components["schemas"];

export type Factory = Schemas["Factory"];
export type Station = Schemas["Station"];
export type StationKind = Schemas["StationKind"];
export type StationAction = Schemas["StationAction"];
export type Item = Schemas["Item"];
export type ItemCard = Schemas["ItemCard"];
export type ItemPage = Schemas["ItemPage"];
export type ItemState = Schemas["ItemState"];
export type ClassOfService = Schemas["ClassOfService"];
export type Board = Schemas["Board"];
export type BoardStation = Schemas["BoardStation"];
export type Metrics = Schemas["Metrics"];
export type ItemEvent = Schemas["Event"];
export type EventPage = Schemas["EventPage"];
export type StreamEvent = Schemas["StreamEvent"];
export type Problem = Schemas["Problem"];

/** createGoalClient returns a typed fetch client for goald at baseUrl ("" for the same origin). */
export function createGoalClient(baseUrl: string, options: Omit<ClientOptions, "baseUrl"> = {}) {
  return createClient<paths>({ ...options, baseUrl });
}

export type GoalClient = ReturnType<typeof createGoalClient>;

/** What a stream subscriber is told. */
export interface StreamHandlers {
  /** An item event was committed. */
  onItem: (event: StreamEvent) => void;
  /**
   * Events may have been missed: on every (re)connection, and when goald lost its own database
   * connection. Re-read whatever the screen shows.
   */
  onResync?: () => void;
  /** The connection failed; EventSource reconnects by itself. */
  onError?: (event: Event) => void;
}

/**
 * subscribe follows /v1/stream with an EventSource, for one factory (id or name) or all of
 * them, and returns the function that stops it.
 */
export function subscribe(baseUrl: string, handlers: StreamHandlers, factory?: string): () => void {
  const query = factory ? `?${new URLSearchParams({ factory }).toString()}` : "";
  const source = new EventSource(`${baseUrl.replace(/\/+$/, "")}/v1/stream${query}`);

  source.addEventListener("open", () => handlers.onResync?.());
  source.addEventListener("resync", () => handlers.onResync?.());
  source.addEventListener("item", (message: MessageEvent<string>) => {
    handlers.onItem(JSON.parse(message.data) as StreamEvent);
  });
  source.addEventListener("error", (event) => handlers.onError?.(event));

  return () => source.close();
}

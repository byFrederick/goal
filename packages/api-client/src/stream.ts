// An organization's event stream (GET /v1/orgs/{org}/stream, text/event-stream). goald does not
// replay: after a reconnect, and on `event: resync`, events may have been missed, so the
// subscriber re-reads what it shows. EventSource reconnects by itself (goald sends `retry: 3000`).
import { orgPath, routeUrl } from "./routes";
import type { components } from "./schema";

export type StreamEvent = components["schemas"]["StreamEvent"];

/** The state of the connection: opening, open, or lost and retrying. */
export type StreamStatus = "connecting" | "live" | "reconnecting";

/** What the stream is filtered to (C §10.10). */
export interface StreamScope {
  /** The organization (its slug or id): a stream carries only its events. */
  org: string;
  /** Only this factory's events (its id or name); every factory's when absent. */
  factory?: string;
}

/** What a stream subscriber is told. */
export interface StreamHandlers {
  /** An item event was committed (`event: item`). */
  onItem: (event: StreamEvent) => void;
  /**
   * Events of the names in `events`, which later phases add to the stream (definition, factory,
   * ...). EventSource only delivers the named events it listens for; names nobody listens for
   * are ignored, so new names never break an older client.
   */
  onEvent?: (name: string, data: unknown) => void;
  /** The other event names to listen for and pass to `onEvent`. */
  events?: readonly string[];
  /**
   * Events may have been missed: on `event: resync`, and on every reconnect after the first
   * open (never on the first open itself, which has missed nothing). Re-read what the screen shows.
   */
  onResync?: () => void;
  /** The connection changed state; called with "connecting" before the first open. */
  onStatus?: (status: StreamStatus) => void;
}

function parse(message: MessageEvent<string>): { ok: true; data: unknown } | { ok: false } {
  try {
    return { ok: true, data: JSON.parse(message.data) as unknown };
  } catch {
    return { ok: false };
  }
}

/**
 * subscribe follows the event stream with an EventSource and returns the function that stops
 * it. A malformed event is skipped: the next one, or the refetch after a resync, catches up.
 */
export function subscribe(baseUrl: string, scope: StreamScope, handlers: StreamHandlers): () => void {
  const source = new EventSource(routeUrl(baseUrl, orgPath("/stream"), { ...scope }));
  handlers.onStatus?.("connecting");

  let opened = false;
  source.addEventListener("open", () => {
    if (opened) handlers.onResync?.();
    opened = true;
    handlers.onStatus?.("live");
  });
  source.addEventListener("error", () => handlers.onStatus?.("reconnecting"));
  source.addEventListener("resync", () => handlers.onResync?.());
  source.addEventListener("item", (message: MessageEvent<string>) => {
    const event = parse(message);
    if (event.ok) handlers.onItem(event.data as StreamEvent);
  });

  for (const name of handlers.events ?? []) {
    source.addEventListener(name, (message: MessageEvent<string>) => {
      const event = parse(message);
      if (event.ok) handlers.onEvent?.(name, event.data);
    });
  }

  return () => source.close();
}

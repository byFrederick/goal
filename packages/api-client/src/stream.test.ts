import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { type StreamEvent, type StreamHandlers, type StreamStatus, subscribe } from "./stream";

// A stand-in for the browser's EventSource: the test plays the server's side of the connection.
class FakeEventSource extends EventTarget {
  static last: FakeEventSource | undefined;
  readonly url: string;
  closed = false;

  constructor(url: string) {
    super();
    this.url = url;
    FakeEventSource.last = this;
  }

  open() {
    this.dispatchEvent(new Event("open"));
  }

  fail() {
    this.dispatchEvent(new Event("error"));
  }

  send(name: string, data: string) {
    this.dispatchEvent(new MessageEvent(name, { data }));
  }

  close() {
    this.closed = true;
  }
}

const event: StreamEvent = {
  org_id: "019a1c00-0000-7000-8000-0000000000aa",
  item_id: "0b6c1a52-3f43-4f53-9a43-0a8c33f3f1a1",
  ref: "goal#12",
  factory_id: "5d2b8f0e-6f1d-4a39-9d43-2d3c9b1e7a10",
  kind: "moved",
  station: "review",
  state: "waiting_human",
  at: "2026-10-23T10:00:00Z",
};

function follow(extra: Partial<StreamHandlers> = {}) {
  const log = {
    items: [] as StreamEvent[],
    others: [] as [string, unknown][],
    resyncs: 0,
    statuses: [] as StreamStatus[],
  };
  const stop = subscribe(
    "http://goald.example.com/",
    { org: "acme", factory: "goal" },
    {
      onItem: (e) => log.items.push(e),
      onEvent: (name, data) => log.others.push([name, data]),
      onResync: () => {
        log.resyncs++;
      },
      onStatus: (s) => log.statuses.push(s),
      ...extra,
    },
  );
  const source = FakeEventSource.last;
  if (!source) throw new Error("subscribe opened no EventSource");
  return { log, source, stop };
}

beforeEach(() => {
  vi.stubGlobal("EventSource", FakeEventSource);
});

afterEach(() => {
  vi.unstubAllGlobals();
  FakeEventSource.last = undefined;
});

describe("subscribe", () => {
  test("opens the stream of the scope", () => {
    const { source } = follow();
    expect(source.url).toBe("http://goald.example.com/v1/orgs/acme/stream?factory=goal");
  });

  test("the first open is not a resync; every reopen after it is", () => {
    const { log, source } = follow();
    expect(log.statuses).toEqual(["connecting"]);

    source.open();
    expect(log.resyncs).toBe(0);
    expect(log.statuses).toEqual(["connecting", "live"]);

    source.fail();
    source.open();
    expect(log.resyncs).toBe(1);
    expect(log.statuses).toEqual(["connecting", "live", "reconnecting", "live"]);
  });

  test("event: resync asks for a re-read", () => {
    const { log, source } = follow();
    source.open();
    source.send("resync", "{}");
    expect(log.resyncs).toBe(1);
  });

  test("item events reach onItem and malformed ones are skipped", () => {
    const { log, source } = follow();
    source.send("item", "{not json");
    source.send("item", JSON.stringify(event));
    expect(log.items).toEqual([event]);
  });

  test("named events reach onEvent; names nobody listens for are ignored", () => {
    const { log, source } = follow({ events: ["definition"] });
    source.send("definition", JSON.stringify({ factory_id: event.factory_id }));
    source.send("credential", JSON.stringify({ id: "c1" }));
    expect(log.others).toEqual([["definition", { factory_id: event.factory_id }]]);
    expect(log.items).toEqual([]);
  });

  test("the returned function closes the stream", () => {
    const { source, stop } = follow();
    stop();
    expect(source.closed).toBe(true);
  });
});

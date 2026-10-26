import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import type { StreamStatus } from '../api/client';
import { followPresence, graceMs, type StreamLink } from './stream-presence';

// A stand-in for one subscription: the test plays the stream's side of it.
class FakeStream {
  closed = false;
  constructor(readonly link: StreamLink) {}
  live() {
    this.link.onStatus('live');
  }
  fail() {
    this.link.onStatus('reconnecting');
  }
  resync() {
    this.link.onResync();
  }
}

function fakePage() {
  let hidden = false;
  const listeners = new Set<(fromCache: boolean) => void>();
  return {
    hidden: () => hidden,
    onShow(listener: (fromCache: boolean) => void) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    hide() {
      hidden = true;
    },
    show(fromCache = false) {
      hidden = false;
      for (const listener of listeners) listener(fromCache);
    },
  };
}

function setup() {
  const streams: FakeStream[] = [];
  const page = fakePage();
  const reported: StreamStatus[] = [];
  let resyncs = 0;
  const stop = followPresence({
    connect: (link) => {
      const stream = new FakeStream(link);
      streams.push(stream);
      return () => {
        stream.closed = true;
      };
    },
    page,
    onStatus: (status) => reported.push(status),
    onResync: () => {
      resyncs++;
    },
  });
  return {
    stop,
    page,
    reported,
    streams,
    current: () => {
      const stream = streams.at(-1);
      if (!stream) throw new Error('no stream opened');
      return stream;
    },
    resyncs: () => resyncs,
  };
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe('followPresence', () => {
  test('return from background reconnects without showing reconnecting', () => {
    const s = setup();
    s.current().live();
    s.page.hide();
    s.current().fail();
    vi.advanceTimersByTime(graceMs * 2);

    s.page.show();
    expect(s.streams).toHaveLength(2);
    expect(s.streams.at(0)?.closed).toBe(true);
    s.current().live();
    vi.advanceTimersByTime(graceMs * 2);

    expect(s.reported).toEqual(['live']);
    expect(s.resyncs()).toBe(1);
  });

  test('a restored page reopens its stream even when it was live', () => {
    const s = setup();
    s.current().live();
    s.page.show(true);
    expect(s.streams).toHaveLength(2);
    expect(s.streams.at(0)?.closed).toBe(true);
  });

  test('outage shorter than grace stays silent', () => {
    const s = setup();
    s.current().live();
    s.current().fail();
    vi.advanceTimersByTime(graceMs - 1);
    s.current().live();
    vi.advanceTimersByTime(graceMs * 2);

    expect(s.reported).toEqual(['live']);
  });

  test('outage longer than grace reports reconnecting', () => {
    const s = setup();
    s.current().live();
    s.current().fail();
    vi.advanceTimersByTime(graceMs - 1);
    expect(s.reported).toEqual(['live']);

    vi.advanceTimersByTime(1);
    expect(s.reported).toEqual(['live', 'reconnecting']);
  });

  test('stream back clears reconnecting and resyncs', () => {
    const s = setup();
    s.current().live();
    s.current().fail();
    vi.advanceTimersByTime(graceMs);
    s.current().resync();
    s.current().live();

    expect(s.reported).toEqual(['live', 'reconnecting', 'live']);
    expect(s.resyncs()).toBe(1);
  });

  test('a hidden page does not report reconnecting when its outage outlasts the grace period', () => {
    const s = setup();
    s.current().live();
    s.current().fail();
    s.page.hide();
    vi.advanceTimersByTime(graceMs * 2);

    expect(s.reported).toEqual(['live']);
  });

  test('stop closes the open stream', () => {
    const s = setup();
    s.current().live();
    s.stop();
    expect(s.current().closed).toBe(true);
  });
});

import { describe, expect, it, vi } from 'vitest';
import type { WorkerEvent } from './events.js';
import { fetchNotes, NotePoller, PromptQueue } from './notes.js';

const note = (id: number, text = `note ${id}`) => ({ id, at: '2026-10-23T10:01:00Z', by: 'alice@example.com', text });

function answer(body: unknown, status = 200) {
  return new Response(typeof body === 'string' ? body : JSON.stringify(body), { status });
}

function poller(fetch: typeof globalThis.fetch, opts: { intervalMs?: number; now?: () => Date } = {}) {
  const inbox = new PromptQueue();
  const events: WorkerEvent[] = [];
  const deps = {
    fetch,
    gatewayUrl: 'http://host.docker.internal:8081/mcp',
    token: 'gateway-token',
    inbox,
    emit: (e: WorkerEvent) => events.push(e),
    now: opts.now ?? (() => new Date('2026-10-23T10:00:00Z')),
    intervalMs: opts.intervalMs ?? 0,
  };
  return { poller: new NotePoller(deps), inbox, events };
}

/** Reads what the inbox yields until it ends. */
async function drain(inbox: PromptQueue): Promise<unknown[]> {
  const out: unknown[] = [];
  for await (const m of inbox) out.push(m);
  return out;
}

describe('fetchNotes', () => {
  it("asks the gateway's /run for the notes after the id, with the run's task token", async () => {
    const fetch = vi.fn(async () => answer({ notes: [note(9)] })) as unknown as typeof globalThis.fetch;

    const notes = await fetchNotes(fetch, 'http://host.docker.internal:8081/mcp', 'gateway-token', 7);

    expect(notes).toEqual([note(9)]);
    const [url, init] = vi.mocked(fetch).mock.calls[0] as unknown as [URL, RequestInit];
    expect(url.href).toBe('http://host.docker.internal:8081/run?notes_after=7');
    expect(init.headers).toEqual({ Authorization: 'Bearer gateway-token' });
  });

  it('rejects on a non-2xx answer and on a body that is not the notes list', async () => {
    const down = (async () => answer('nope', 500)) as typeof globalThis.fetch;
    const garbled = (async () => answer({ items: [] })) as typeof globalThis.fetch;

    await expect(fetchNotes(down, 'http://gw.example.com/', 't', 0)).rejects.toThrow('500');
    await expect(fetchNotes(garbled, 'http://gw.example.com/', 't', 0)).rejects.toThrow();
  });
});

describe('NotePoller', () => {
  it('pushes each new note once as a priority-next user message and reports it delivered', async () => {
    const {
      poller: p,
      inbox,
      events,
    } = poller((async () => answer({ notes: [note(3, 'first'), note(5, 'second')] })) as typeof globalThis.fetch);

    await p.poll();
    inbox.close();

    const messages = (await drain(inbox)) as { message: { content: string }; priority: string }[];
    expect(messages.map((m) => [m.priority, m.message.content])).toEqual([
      ['next', 'A note from alice@example.com arrived while you work: first'],
      ['next', 'A note from alice@example.com arrived while you work: second'],
    ]);
    expect(events).toEqual([
      { type: 'note_delivered', at: '2026-10-23T10:00:00.000Z', note_id: 3 },
      { type: 'note_delivered', at: '2026-10-23T10:00:00.000Z', note_id: 5 },
    ]);
  });

  it('builds the user message the agent reads: who left the note and its text', async () => {
    const { poller: p, inbox } = poller((async () =>
      answer({ notes: [note(4, 'Use v2')] })) as typeof globalThis.fetch);

    await p.poll();
    inbox.close();

    expect(await drain(inbox)).toEqual([
      {
        type: 'user',
        message: { role: 'user', content: 'A note from alice@example.com arrived while you work: Use v2' },
        parent_tool_use_id: null,
        priority: 'next',
      },
    ]);
  });

  it('delivers a note once even when the gateway lists it on every poll', async () => {
    const fetch = vi.fn(async () => answer({ notes: [note(8)] })) as unknown as typeof globalThis.fetch;
    const { poller: p, inbox, events } = poller(fetch);

    await p.poll();
    await p.poll();
    await p.poll();
    inbox.close();

    expect(await drain(inbox)).toHaveLength(1);
    expect(events.filter((e) => e.type === 'note_delivered')).toHaveLength(1);
    // The cursor moved to the note, so the later polls ask for what came after it.
    const asked = vi.mocked(fetch).mock.calls.map(([url]) => new URL(String(url)).searchParams.get('notes_after'));
    expect(asked).toEqual(['0', '8', '8']);
  });

  it('polls at most once per interval', async () => {
    let at = 0;
    const fetch = vi.fn(async () => answer({ notes: [] })) as unknown as typeof globalThis.fetch;
    const { poller: p } = poller(fetch, { intervalMs: 15_000, now: () => new Date(at) });

    await p.poll();
    at = 14_999;
    await p.poll();
    expect(fetch).toHaveBeenCalledTimes(1);

    at = 15_000;
    await p.poll();
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it('writes one line to stderr and delivers nothing when the poll fails', async () => {
    const write = vi.spyOn(process.stderr, 'write').mockImplementation(() => true);
    const { poller: p, inbox, events } = poller((async () => answer('down', 503)) as typeof globalThis.fetch);

    await expect(p.poll()).resolves.toBeUndefined();
    inbox.close();

    expect(await drain(inbox)).toEqual([]);
    expect(events).toEqual([]);
    expect(write).toHaveBeenCalledOnce();
    expect(write.mock.calls[0]?.[0]).toMatch(/^goal-agent: notes not fetched: .*503/);
    write.mockRestore();
  });
});

describe('PromptQueue', () => {
  it('yields what was pushed in order, waits for more, and ends once closed and drained', async () => {
    const queue = new PromptQueue();
    const seen: unknown[] = [];
    const reading = (async () => {
      for await (const m of queue) seen.push(m);
    })();

    queue.push({ type: 'user', message: { role: 'user', content: 'a' }, parent_tool_use_id: null });
    await new Promise((resolve) => setImmediate(resolve));
    queue.push({ type: 'user', message: { role: 'user', content: 'b' }, parent_tool_use_id: null });
    queue.close();
    queue.push({ type: 'user', message: { role: 'user', content: 'late' }, parent_tool_use_id: null });
    await reading;

    expect(seen.map((m) => (m as { message: { content: string } }).message.content)).toEqual(['a', 'b']);
  });
});

import type { SDKUserMessage } from '@anthropic-ai/claude-agent-sdk';
import { z } from 'zod';
import type { WorkerEvent } from './events.js';

/** How often the worker asks the gateway for new notes between turns, unless the run says otherwise. */
export const notePollMs = 15_000;

/** A poll that has not answered by then is given up: a slow gateway must not stall the agent. */
const pollTimeoutMs = 10_000;

const noteList = z.object({
  notes: z.array(z.object({ id: z.number().int(), at: z.string(), by: z.string(), text: z.string() })),
});

/** A note as goald's gateway lists it (GET /run?notes_after, internal/gateway/notes.go). */
export type Note = z.infer<typeof noteList>['notes'][number];

/**
 * Fetches the run's notes after the id given, oldest first, from goald's gateway with the run's
 * task token. It rejects on a network error, a non-2xx answer or a body that does not parse.
 */
export async function fetchNotes(
  fetch: typeof globalThis.fetch,
  gatewayUrl: string,
  token: string,
  after: number,
): Promise<Note[]> {
  const url = new URL('/run', gatewayUrl);
  url.searchParams.set('notes_after', String(after));
  const res = await fetch(url, {
    headers: { Authorization: `Bearer ${token}` },
    signal: AbortSignal.timeout(pollTimeoutMs),
  });
  if (!res.ok) throw new Error(`the gateway answered ${res.status}`);
  return noteList.parse(await res.json()).notes;
}

/**
 * The prompt the SDK reads as streaming input: it yields the messages pushed to it, in order, and
 * ends once close is called and they are all read. Until then it waits.
 */
export class PromptQueue implements AsyncIterable<SDKUserMessage> {
  readonly #items: SDKUserMessage[] = [];
  #closed = false;
  #wake: (() => void) | undefined = undefined;

  push(message: SDKUserMessage): void {
    if (this.#closed) return;
    this.#items.push(message);
    this.#wake?.();
  }

  close(): void {
    this.#closed = true;
    this.#wake?.();
  }

  async *[Symbol.asyncIterator](): AsyncIterator<SDKUserMessage> {
    for (;;) {
      while (this.#items.length > 0) yield this.#items.shift() as SDKUserMessage;
      if (this.#closed) return;
      await new Promise<void>((resolve) => {
        this.#wake = resolve;
      });
      this.#wake = undefined;
    }
  }
}

export interface NotePollerDeps {
  fetch: typeof globalThis.fetch;
  gatewayUrl: string;
  token: string;
  inbox: PromptQueue;
  emit: (event: WorkerEvent) => void;
  now: () => Date;
  intervalMs: number;
}

/**
 * Brings the run's new notes to the agent. Each poll asks for the notes after the last id seen,
 * pushes each new one to the inbox as a user message that reaches the agent between its turns,
 * and reports it with note_delivered. A failed poll is one line on stderr and the run goes on.
 */
export class NotePoller {
  readonly #deps: NotePollerDeps;
  #after = 0;
  #polledAt?: number;

  constructor(deps: NotePollerDeps) {
    this.#deps = deps;
  }

  /** Polls unless the last poll was less than the interval ago. */
  async poll(): Promise<void> {
    const { now, intervalMs } = this.#deps;
    const at = now().getTime();
    if (this.#polledAt !== undefined && at - this.#polledAt < intervalMs) return;
    this.#polledAt = at;

    let notes: Note[];
    try {
      notes = await fetchNotes(this.#deps.fetch, this.#deps.gatewayUrl, this.#deps.token, this.#after);
    } catch (err) {
      process.stderr.write(`goal-agent: notes not fetched: ${err instanceof Error ? err.message : String(err)}\n`);
      return;
    }

    for (const note of notes) {
      // A note is delivered once: a gateway that lists it again, or one older than the last id
      // seen, is skipped.
      if (note.id <= this.#after) continue;
      this.#after = note.id;
      this.#deps.inbox.push(noteMessage(note));
      this.#deps.emit({ type: 'note_delivered', at: this.#deps.now().toISOString(), note_id: note.id });
    }
  }
}

/** The user message that carries a note to the agent, at priority next: it reaches the running turn. */
export function noteMessage(note: Note): SDKUserMessage {
  return {
    type: 'user',
    message: { role: 'user', content: `A note from ${note.by} arrived while you work: ${note.text}` },
    parent_tool_use_id: null,
    priority: 'next',
  };
}

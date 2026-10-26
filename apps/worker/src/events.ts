import type { SDKMessage, SDKResultMessage } from '@anthropic-ai/claude-agent-sdk';

/**
 * The worker's stdout protocol: one JSON object per line. goald decodes it with
 * domain.DecodeWorkerLine (internal/domain/worker.go); keep both sides in step.
 */
export type WorkerEvent =
  | {
      type: 'started';
      at: string;
      session_id: string;
      model: string;
      tools: string[];
      skills: string[];
      mcp_servers: { name: string; status: string }[];
    }
  | { type: 'tool_use'; at: string; id: string; tool: string; input: string }
  | { type: 'tool_result'; at: string; id: string; is_error: boolean; summary: string }
  | { type: 'message'; at: string; text: string }
  | {
      type: 'usage';
      at: string;
      input_tokens: number;
      output_tokens: number;
      cache_read_input_tokens: number;
      cache_creation_input_tokens: number;
    }
  /** A note a person left on the item during the run, handed to the agent as a user message. */
  | { type: 'note_delivered'; at: string; note_id: number }
  | Finished;

export interface Finished {
  type: 'finished';
  at: string;
  status: 'succeeded' | 'failed';
  /** The SDK's result subtype, for example error_max_turns. */
  subtype?: string;
  /** The final JSON output; present when status is succeeded. */
  output?: unknown;
  error?: string;
  cost_usd: number;
  turns: number;
  input_tokens: number;
  output_tokens: number;
  session_id: string;
  /** The tool calls the agent's policy refused, by tool and call id (never their input); absent when none. */
  permission_denials?: { tool: string; id: string }[];
}

/** Progress text is cut to these lengths; events are for watching, not for replaying. */
const maxInput = 500;
const maxSummary = 500;
const maxText = 2000;

export function clip(s: string, max: number): string {
  return s.length > max ? `${s.slice(0, max)}…` : s;
}

type Usage = Omit<Extract<WorkerEvent, { type: 'usage' }>, 'type' | 'at'>;

/**
 * Turns SDK messages into protocol events. Usage comes from the stream: the assistant messages
 * carry only the opening counts of their API message (output_tokens 1), the final ones arrive in
 * its message_delta, so the mapper keeps each open API message's opening counts, by session
 * thread (the main one or a subagent's tool call).
 */
export class EventMapper {
  readonly #now: () => Date;
  readonly #opened = new Map<string | null, Usage>();
  readonly #skills: ReadonlySet<string>;

  /** skills are the ones the agent may use; Claude Code's init lists every skill it found. */
  constructor(now: () => Date, skills: readonly string[]) {
    this.#now = now;
    this.#skills = new Set(skills);
  }

  map(msg: SDKMessage): WorkerEvent[] {
    const at = this.#now().toISOString();
    switch (msg.type) {
      case 'system':
        if (msg.subtype !== 'init') return [];
        return [
          {
            type: 'started',
            at,
            session_id: msg.session_id,
            model: msg.model,
            tools: msg.tools,
            skills: msg.skills.filter((name) => this.#skills.has(name)),
            mcp_servers: msg.mcp_servers.map(({ name, status }) => ({ name, status })),
          },
        ];
      case 'assistant':
        return this.#assistant(msg.message, at);
      case 'user':
        return toolResults(msg.message.content, at);
      case 'stream_event':
        return this.#usage(msg.event, msg.parent_tool_use_id, at);
      case 'result':
        return [finished(msg, at)];
      default:
        return [];
    }
  }

  #assistant(message: Extract<SDKMessage, { type: 'assistant' }>['message'], at: string): WorkerEvent[] {
    const events: WorkerEvent[] = [];
    for (const block of message.content) {
      if (block.type === 'text' && block.text.trim() !== '') {
        events.push({ type: 'message', at, text: clip(block.text, maxText) });
      } else if (block.type === 'tool_use') {
        events.push({
          type: 'tool_use',
          at,
          id: block.id,
          tool: block.name,
          input: clip(JSON.stringify(block.input), maxInput),
        });
      }
    }
    return events;
  }

  /** One usage event per API message, when its message_delta brings the final counts. */
  #usage(
    event: Extract<SDKMessage, { type: 'stream_event' }>['event'],
    thread: string | null,
    at: string,
  ): WorkerEvent[] {
    if (event.type === 'message_start') {
      const u = event.message.usage;
      this.#opened.set(thread, {
        input_tokens: u.input_tokens,
        output_tokens: u.output_tokens,
        cache_read_input_tokens: u.cache_read_input_tokens ?? 0,
        cache_creation_input_tokens: u.cache_creation_input_tokens ?? 0,
      });
      return [];
    }
    if (event.type !== 'message_delta') return [];
    const opened = this.#opened.get(thread);
    if (!opened) return [];
    this.#opened.delete(thread);
    const u = event.usage;
    return [
      {
        type: 'usage',
        at,
        input_tokens: u.input_tokens ?? opened.input_tokens,
        output_tokens: u.output_tokens,
        cache_read_input_tokens: u.cache_read_input_tokens ?? opened.cache_read_input_tokens,
        cache_creation_input_tokens: u.cache_creation_input_tokens ?? opened.cache_creation_input_tokens,
      },
    ];
  }
}

type UserContent = Extract<SDKMessage, { type: 'user' }>['message']['content'];

function toolResults(content: UserContent, at: string): WorkerEvent[] {
  if (typeof content === 'string') return [];
  return content.flatMap((block): WorkerEvent[] =>
    block.type === 'tool_result'
      ? [
          {
            type: 'tool_result',
            at,
            id: block.tool_use_id,
            is_error: block.is_error ?? false,
            summary: clip(resultText(block.content), maxSummary),
          },
        ]
      : [],
  );
}

function resultText(content: unknown): string {
  if (typeof content === 'string') return content;
  if (!Array.isArray(content)) return '';
  return content
    .map((part: { type?: string; text?: string }) =>
      part.type === 'text' ? (part.text ?? '') : `[${part.type ?? 'content'}]`,
    )
    .join('\n');
}

export function finished(msg: SDKResultMessage, at: string): Finished {
  const base = {
    type: 'finished' as const,
    at,
    subtype: msg.subtype,
    cost_usd: msg.total_cost_usd,
    turns: msg.num_turns,
    input_tokens: msg.usage.input_tokens,
    output_tokens: msg.usage.output_tokens,
    session_id: msg.session_id,
    ...denials(msg.permission_denials),
  };
  if (msg.subtype !== 'success') {
    return { ...base, status: 'failed', error: msg.errors.join('; ') || msg.subtype };
  }
  if (msg.is_error) {
    return { ...base, status: 'failed', error: msg.result || 'the agent ended with an error' };
  }
  return { ...base, status: 'succeeded', output: msg.structured_output ?? parseOutput(msg.result) };
}

/** The denied calls by tool and call id; their input may hold anything, so it stays in the container. */
function denials(list: SDKResultMessage['permission_denials'] | undefined): Pick<Finished, 'permission_denials'> {
  if (!list || list.length === 0) return {};
  return { permission_denials: list.map((d) => ({ tool: d.tool_name, id: d.tool_use_id })) };
}

/**
 * Reads the agent's final reply as JSON: the whole reply, a fenced ```json block or the outermost
 * object. A reply that holds no JSON is passed on as a JSON string, so the station's contract
 * check reports what is wrong with it.
 */
export function parseOutput(text: string): unknown {
  const candidates = [text, /```(?:json)?\s*\n([\s\S]*?)\n```/.exec(text)?.[1], outermostObject(text)];
  for (const candidate of candidates) {
    if (candidate === undefined) continue;
    try {
      return JSON.parse(candidate);
    } catch {
      // try the next reading
    }
  }
  return text;
}

function outermostObject(text: string): string | undefined {
  const start = text.indexOf('{');
  const end = text.lastIndexOf('}');
  return start >= 0 && end > start ? text.slice(start, end + 1) : undefined;
}

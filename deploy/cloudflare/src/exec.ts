// One command in a box, as api/sandbox.yaml streams it: standard output as it is written, then how
// the command ended.

/** The last bytes of standard error kept (ports.MaxStderrTail). */
export const maxStderrTail = 8 << 10;

/** Seconds between the SIGTERM a timeout or a kill sends and the SIGKILL that follows. */
export const killAfterSeconds = 10;

/** The exit codes of GNU timeout for a command it timed out: SIGTERM, then SIGKILL. */
const timedOutCodes = new Set([124, 137]);

/**
 * wrapped runs argv under GNU coreutils' timeout, which kills the whole process group: the
 * runtime's kill reaches only the process it started (spec 4 §5.1). 0 is no timeout.
 */
export function wrapped(argv: readonly string[], timeoutSeconds: number): string[] {
  return ['timeout', `--kill-after=${killAfterSeconds}`, String(timeoutSeconds), ...argv];
}

/** A running command the Durable Object can kill, or end when its box is destroyed. */
export interface Running {
  /** kill asks timeout to end the command's process group (SIGTERM, SIGKILL after 10 s). */
  kill(): void;
  /** end finishes the stream with an unknown outcome: the box is gone. */
  end(reason: string): void;
}

/**
 * How long a command's outcome is kept once it ended, for a reader that attaches again: a cut on
 * the way between goald and the box is not the end of a command, and goald attaches again where it
 * stopped reading (spec 4 §5.12 check 4). The Worker never learns that a reader left, so a command
 * runs until it ends, goald kills it or the box goes.
 */
export const keepOutcomeMs = 2 * 60_000;

/** The output kept for a reader that attaches again: its last bytes. */
export const maxReplayBytes = 4 << 20;

/** Gone is an attach at an offset whose output is no longer kept, or not yet written. */
export class Gone extends Error {}

/** One piece of output and the byte offset it starts at. */
interface Piece {
  at: number;
  text: string;
  bytes: number;
}

/**
 * Exec is one command in a box, as api/sandbox.yaml streams it: its output, read as it is written
 * and kept for the last maxReplayBytes, and its outcome. One reader at a time attaches at a byte
 * offset of the output, the newest; done runs keepMs after the command ended.
 */
export class Exec implements Running {
  private readonly pieces: Piece[] = [];
  private written = 0;
  private outcome: Record<string, unknown> | undefined;
  private exited = false;
  private reader: { close(): void } | undefined;
  private waiters: (() => void)[] = [];
  private fail: (reason: Error) => void = () => {};

  constructor(
    private readonly process: ExecProcess,
    timeoutSeconds: number,
    private readonly keepMs: number,
    private readonly done: () => void,
  ) {
    void this.pump(timeoutSeconds);
  }

  kill(): void {
    if (this.exited) return; // signalling a process that exited is an internal error
    try {
      this.process.kill(15);
    } catch {
      // it exited meanwhile
    }
  }

  end(reason: string): void {
    this.fail(new Error(reason));
  }

  /** attach streams the output from byte offset, then the outcome. Gone when offset is not kept. */
  attach(offset: number): ReadableStream<Uint8Array> {
    const kept = this.pieces[0]?.at ?? this.written;
    if (offset < kept || offset > this.written) {
      throw new Gone(`output from byte ${offset} is not kept (bytes ${kept} to ${this.written} are)`);
    }
    this.reader?.close(); // one reader at a time: the newest

    const encode = new TextEncoder();
    const line = (value: unknown) => encode.encode(`${JSON.stringify(value)}\n`);
    let cursor = offset;
    let closed = false;
    let controller: ReadableStreamDefaultController<Uint8Array> | undefined;
    const me = {
      close() {
        if (closed) return;
        closed = true;
        try {
          controller?.close();
        } catch {
          // already closed
        }
      },
    };
    this.reader = me;

    return new ReadableStream<Uint8Array>({
      start: (c) => {
        controller = c;
      },
      pull: async (c) => {
        for (;;) {
          if (closed) return;
          const piece = this.from(cursor);
          if (piece) {
            c.enqueue(line({ o: piece.text }));
            cursor = piece.end;
            return;
          }
          if (this.outcome) {
            c.enqueue(line(this.outcome));
            me.close();
            if (this.reader === me) this.reader = undefined;
            return;
          }
          await new Promise<void>((resolve) => this.waiters.push(resolve));
        }
      },
      cancel: () => {
        closed = true;
        if (this.reader === me) this.reader = undefined;
      },
    });
  }

  /** pump reads the command's output into pieces until it ends, then records how it ended. */
  private async pump(timeoutSeconds: number): Promise<void> {
    const started = Date.now();
    const ended = new Promise<never>((_, reject) => {
      this.fail = reject;
    });
    ended.catch(() => {});
    const exitCode = this.process.exitCode.finally(() => {
      this.exited = true;
    });
    // Both settle once the box is gone, even after the stream has ended without them.
    exitCode.catch(() => {});
    const stderr = tail(this.process.stderr, maxStderrTail);
    stderr.catch(() => {});

    const text = new TextDecoder();
    try {
      const out = this.process.stdout?.getReader();
      for (;;) {
        const next = out ? await Promise.race([out.read(), ended]) : { done: true as const, value: undefined };
        if (next.done) break;
        this.add(text.decode(next.value as Uint8Array, { stream: true }));
      }
      this.add(text.decode());
      const [code, tailText] = await Promise.race([Promise.all([exitCode, stderr]), ended]);
      const timedOut =
        timeoutSeconds > 0 && timedOutCodes.has(code) && Date.now() - started >= (timeoutSeconds - 1) * 1000;
      this.finish({ exit: code, timed_out: timedOut, stderr: tailText });
    } catch (error) {
      this.finish({ error: `the command's outcome is unknown: ${messageOf(error)}` });
    }
  }

  private add(text: string): void {
    if (text === '') return;
    const bytes = new TextEncoder().encode(text).length;
    this.pieces.push({ at: this.written, text, bytes });
    this.written += bytes;
    while (this.pieces.length > 1 && this.written - (this.pieces[0]?.at ?? 0) > maxReplayBytes) this.pieces.shift();
    this.wake();
  }

  private finish(outcome: Record<string, unknown>): void {
    this.outcome = outcome;
    this.wake();
    setTimeout(this.done, this.keepMs);
  }

  /** from is the output from byte cursor to the end of its piece, if any is written. */
  private from(cursor: number): { text: string; end: number } | undefined {
    let lo = 0;
    let hi = this.pieces.length - 1;
    while (lo <= hi) {
      const mid = (lo + hi) >> 1;
      const p = this.pieces[mid] as Piece;
      if (cursor < p.at) hi = mid - 1;
      else if (cursor >= p.at + p.bytes) lo = mid + 1;
      else {
        const text =
          cursor === p.at ? p.text : new TextDecoder().decode(new TextEncoder().encode(p.text).slice(cursor - p.at));
        return { text, end: p.at + p.bytes };
      }
    }
    return undefined;
  }

  private wake(): void {
    const waiters = this.waiters;
    this.waiters = [];
    for (const w of waiters) w();
  }
}

/** tail reads a stream to its end and keeps its last max bytes, as text. */
async function tail(source: ReadableStream | null, max: number): Promise<string> {
  if (!source) return '';
  let kept = new Uint8Array(0);
  const reader = source.getReader();
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    const chunk = value as Uint8Array;
    const joined = new Uint8Array(kept.length + chunk.length);
    joined.set(kept);
    joined.set(chunk, kept.length);
    kept = joined.length > max ? joined.slice(joined.length - max) : joined;
  }
  return new TextDecoder().decode(kept);
}

/** The box's own environment and working directory: what its image gives its first process. */
export interface Base {
  env: Record<string, string>;
  dir: string;
}

/**
 * The file a box's entrypoint writes before it sleeps: its working directory, a newline, then its
 * environment as /proc lists it (NUL-separated). A command the runtime execs inherits neither
 * (only PATH), so the Worker gives each command this environment under its own (spec 4 §5.1).
 */
export const baseFile = '/tmp/.goal-box';

/** entrypoint is a box's first process: it records the base, then sleeps for seconds. */
export function entrypoint(seconds: number): string[] {
  const script = `{ pwd; cat "/proc/$$/environ"; } > "${baseFile}.part" && mv "${baseFile}.part" "${baseFile}" && exec sleep "$1"`;
  return ['/bin/sh', '-c', script, 'goal-box', String(Math.max(1, Math.ceil(seconds)))];
}

/** parseBase reads the file entrypoint writes. */
export function parseBase(content: string): Base {
  const newline = content.indexOf('\n');
  const dir = newline < 0 ? content : content.slice(0, newline);
  const env: Record<string, string> = {};
  for (const pair of (newline < 0 ? '' : content.slice(newline + 1)).split('\0')) {
    const eq = pair.indexOf('=');
    if (eq > 0) env[pair.slice(0, eq)] = pair.slice(eq + 1);
  }
  return { dir: dir === '' ? '/' : dir, env };
}

export function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

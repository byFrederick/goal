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
 * stream turns a process into the NDJSON answer of an exec and returns it with its handle.
 * done runs once the stream has ended, however it ended.
 */
export function stream(
  process: ExecProcess,
  timeoutSeconds: number,
  done: () => void,
): { body: ReadableStream<Uint8Array>; running: Running } {
  const started = Date.now();
  let exited = false;
  const exitCode = process.exitCode.finally(() => {
    exited = true;
  });
  // Both settle once the box is gone, even after the stream has ended without them.
  exitCode.catch(() => {});

  let fail: (reason: Error) => void = () => {};
  const ended = new Promise<never>((_, reject) => {
    fail = reject;
  });
  ended.catch(() => {});

  const running: Running = {
    kill() {
      if (exited) return; // signalling a process that exited is an internal error
      try {
        process.kill(15);
      } catch {
        // it exited meanwhile
      }
    },
    end(reason) {
      fail(new Error(reason));
    },
  };

  const out = process.stdout?.getReader();
  const stderr = tail(process.stderr, maxStderrTail);
  stderr.catch(() => {});
  const text = new TextDecoder();
  const encode = new TextEncoder();
  const line = (value: unknown) => encode.encode(`${JSON.stringify(value)}\n`);

  let finished = false;
  const finish = () => {
    if (finished) return;
    finished = true;
    done();
  };

  const body = new ReadableStream<Uint8Array>({
    async pull(controller) {
      try {
        const next = out ? await Promise.race([out.read(), ended]) : { done: true as const, value: undefined };
        if (!next.done) {
          const piece = text.decode(next.value as Uint8Array, { stream: true });
          if (piece !== '') controller.enqueue(line({ o: piece }));
          return;
        }
        const rest = text.decode();
        if (rest !== '') controller.enqueue(line({ o: rest }));
        const [code, tailText] = await Promise.race([Promise.all([exitCode, stderr]), ended]);
        const timedOut =
          timeoutSeconds > 0 && timedOutCodes.has(code) && Date.now() - started >= (timeoutSeconds - 1) * 1000;
        controller.enqueue(line({ exit: code, timed_out: timedOut, stderr: tailText }));
      } catch (error) {
        controller.enqueue(line({ error: `the command's outcome is unknown: ${messageOf(error)}` }));
      }
      controller.close();
      finish();
    },
    cancel() {
      // The reader left: nobody will read the outcome, so the command stops too.
      running.kill();
      finish();
    },
  });

  return { body, running };
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

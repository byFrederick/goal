import { mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';

/** Where the box's container sees its own cgroup v2 counters. */
export const cgroupRoot = '/sys/fs/cgroup';

/** The file in the goal directory (/goal) where prepare saves cpu.stat's usage_usec at its start. */
export const cpuStartFile = 'cpu-start-usec';

/** How often a run samples memory.current when the cgroup has no memory.peak. */
export const memorySampleMs = 5_000;

/** What a run used, for the finished line; a field whose counters could not be read is absent. */
export interface Usage {
  /** cpu.stat's usage_usec at the run's end less prepare's starting reading, in seconds. */
  cpu_seconds?: number;
  /** memory.peak at the run's end, or the largest memory.current sampled during the run, in bytes. */
  peak_memory_bytes?: number;
}

/** A whole, non-negative count from a counter file's text; undefined when it is anything else. */
function count(text: string): number | undefined {
  const t = text.trim();
  if (!/^\d+$/.test(t)) return undefined;
  const n = Number(t);
  return Number.isSafeInteger(n) ? n : undefined;
}

/** A file's text, or undefined when it is missing or unreadable. */
const read = (path: string) => readFile(path, 'utf8').catch(() => undefined);

/** cpu.stat's usage_usec under root; undefined when the file or the line is missing or does not parse. */
export async function readCpuUsec(root: string): Promise<number | undefined> {
  const text = await read(join(root, 'cpu.stat'));
  for (const line of text?.split('\n') ?? []) {
    const [key, value] = line.trim().split(/\s+/);
    if (key === 'usage_usec' && value !== undefined) return count(value);
  }
  return undefined;
}

/** A memory counter (memory.peak or memory.current) under root, in bytes; undefined when unreadable. */
export async function readMemory(root: string, file: 'memory.peak' | 'memory.current'): Promise<number | undefined> {
  const text = await read(join(root, file));
  return text === undefined ? undefined : count(text);
}

const stderrLine = (line: string) => {
  process.stderr.write(`${line}\n`);
};

/**
 * Saves cpu.stat's usage_usec under root to file, for the run to subtract at its end. A box reused for
 * a retry runs prepare again, so the file is replaced. When the counter can't be read, an earlier
 * file is removed, so the run leaves cpu_seconds out, and one line goes to log. It never throws.
 */
export async function saveCpuStart(root: string, file: string, log: (line: string) => void = stderrLine) {
  try {
    const usec = await readCpuUsec(root);
    if (usec === undefined) {
      await rm(file, { force: true });
      log(`goal-agent prepare: no cgroup CPU counter in ${join(root, 'cpu.stat')}; the run reports no CPU time`);
      return;
    }
    await mkdir(dirname(file), { recursive: true });
    await writeFile(file, String(usec));
  } catch (err) {
    log(`goal-agent prepare: saving the cgroup CPU counter: ${err instanceof Error ? err.message : String(err)}`);
  }
}

export interface MeterOptions {
  /** The cgroup v2 root to read; cgroupRoot in the box. */
  root: string;
  /** The file saveCpuStart wrote at prepare's start. */
  startFile: string;
  /** How often memory.current is sampled; memorySampleMs when absent. */
  sampleMs?: number;
  /** Where the one line about a counter that could not be read goes; stderr when absent. */
  log?: (line: string) => void;
}

/** A run's measurement: stop ends the sampling and returns the usage; every call returns the same. */
export interface ResourceMeter {
  stop(): Promise<Usage>;
}

/**
 * Starts measuring a run in the box's cgroup. Until stop, memory.current is sampled every sampleMs
 * on a timer that never keeps the process alive. stop reads usage_usec and memory.peak once more and
 * returns what could be read: cpu_seconds only with prepare's starting reading, and the largest
 * memory.current seen (one read at stop included) where memory.peak is missing. A counter that
 * can't be read leaves its field out and writes at most one line to log; stop never throws.
 */
export function startMeter(opts: MeterOptions): ResourceMeter {
  let largest: number | undefined;
  const sample = async () => {
    const current = await readMemory(opts.root, 'memory.current');
    if (current !== undefined && (largest === undefined || current > largest)) largest = current;
  };
  void sample();
  const timer = setInterval(() => void sample(), opts.sampleMs ?? memorySampleMs);
  timer.unref();

  let stopped: Promise<Usage> | undefined;
  const measure = async (): Promise<Usage> => {
    clearInterval(timer);
    const usage: Usage = {};
    try {
      const [start, end, peak] = await Promise.all([
        read(opts.startFile).then((t) => (t === undefined ? undefined : count(t))),
        readCpuUsec(opts.root),
        readMemory(opts.root, 'memory.peak'),
        sample(),
      ]);
      if (start !== undefined && end !== undefined && end >= start) usage.cpu_seconds = (end - start) / 1e6;
      const memory = peak ?? largest;
      if (memory !== undefined) usage.peak_memory_bytes = memory;
    } catch {
      // Every read above already turns a failure into undefined; this keeps stop from ever throwing.
    }
    const missing = [
      ...(usage.cpu_seconds === undefined ? ['CPU time'] : []),
      ...(usage.peak_memory_bytes === undefined ? ['peak memory'] : []),
    ];
    if (missing.length > 0) {
      (opts.log ?? stderrLine)(`goal-agent run: no cgroup counter under ${opts.root} for ${missing.join(' or ')}`);
    }
    return usage;
  };
  return {
    stop() {
      stopped ??= measure();
      return stopped;
    },
  };
}

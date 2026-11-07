import { mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';

/** Where the box's container sees its own cgroup v2 counters. */
export const cgroupRoot = '/sys/fs/cgroup';

/** The file naming the worker's own cgroup: its `0::<path>` line is the path under cgroupRoot. */
export const selfCgroupFile = '/proc/self/cgroup';

/** The machine's memory counters, read where no cgroup has a memory counter. */
export const meminfoFile = '/proc/meminfo';

/** The file in the goal directory (/goal) where prepare saves cpu.stat's usage_usec at its start. */
export const cpuStartFile = 'cpu-start-usec';

/** How often a run samples memory.current, and the machine's memory in use, for the largest reading. */
export const memorySampleMs = 5_000;

/** What a run used, for the finished line; a field whose counters could not be read is absent. */
export interface Usage {
  /** cpu.stat's usage_usec at the run's end less prepare's starting reading, in seconds. */
  cpu_seconds?: number;
  /** The run's peak memory in bytes, from the first source startMeter could read. */
  peak_memory_bytes?: number;
  /** Where peak_memory_bytes came from: a cgroup's counters, or the machine's /proc/meminfo; absent with it. */
  memory_source?: MemorySource;
}

/** 'cgroup' for memory.peak or memory.current of a cgroup, 'machine' for MemTotal less MemAvailable. */
export type MemorySource = 'cgroup' | 'machine';

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

/**
 * The worker's own cgroup directory: root joined with the path of the `0::<path>` line in file (the
 * cgroup v2 line of /proc/self/cgroup). Undefined when the file or the line is missing, or the path
 * is `/`, which is root itself.
 */
export async function readSelfCgroup(root: string, file: string): Promise<string | undefined> {
  const text = await read(file);
  for (const line of text?.split('\n') ?? []) {
    if (!line.startsWith('0::')) continue;
    const path = line.slice(3).trim();
    return path.startsWith('/') && path !== '/' ? join(root, path) : undefined;
  }
  return undefined;
}

/** MemTotal less MemAvailable from a /proc/meminfo file, in bytes; undefined when either is missing. */
export async function readMachineMemory(file: string): Promise<number | undefined> {
  const text = await read(file);
  const kb: Record<string, number | undefined> = {};
  for (const line of text?.split('\n') ?? []) {
    const m = /^(MemTotal|MemAvailable):\s+(\d+) kB$/.exec(line.trim());
    if (m?.[1] !== undefined && m[2] !== undefined) kb[m[1]] = count(m[2]);
  }
  const total = kb.MemTotal;
  const available = kb.MemAvailable;
  if (total === undefined || available === undefined || available > total) return undefined;
  return (total - available) * 1024;
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
  /** The file naming the worker's own cgroup under root; selfCgroupFile in the box. Absent, it is not read. */
  selfCgroup?: string;
  /** The machine's memory counters, the last source for peak memory; meminfoFile in the box. Absent, not read. */
  meminfo?: string;
  /** The file saveCpuStart wrote at prepare's start. */
  startFile: string;
  /** How often memory.current is sampled; memorySampleMs when absent. */
  sampleMs?: number;
  /** Where the one line about a counter that could not be read goes; stderr when absent. */
  log?: (line: string) => void;
  /** Reads memory.current under root for a sample; readMemory when absent. Tests wrap it to await each read. */
  readCurrent?: (root: string) => Promise<number | undefined>;
  /** Reads the machine's memory in use for a sample; readMachineMemory when absent. Tests wrap it like readCurrent. */
  readMachine?: (file: string) => Promise<number | undefined>;
}

/** A run's measurement: stop ends the sampling and returns the usage; every call returns the same. */
export interface ResourceMeter {
  stop(): Promise<Usage>;
}

/** The largest of two readings, either of which may be missing. */
const larger = (a: number | undefined, b: number | undefined) =>
  a === undefined ? b : b === undefined ? a : Math.max(a, b);

/**
 * Starts measuring a run in the box. Until stop, memory is sampled every sampleMs on a timer that
 * never keeps the process alive. stop reads usage_usec and the memory counters once more and returns
 * what could be read: cpu_seconds from cpu.stat at root, only with prepare's starting reading; and
 * peak_memory_bytes from the first memory source that reads, in this order:
 *
 * 1. the worker's own cgroup (selfCgroup's `0::<path>` under root, when that path is not `/`):
 *    memory.peak, else the largest memory.current seen;
 * 2. the same files at root;
 * 3. the largest MemTotal less MemAvailable seen in meminfo. A box runs one run at a time, so the
 *    machine's memory in use is the run's.
 *
 * memory_source says which: 'cgroup' for 1 and 2, 'machine' for 3. Every largest reading includes one
 * taken at stop. A field that can't be read is left out with one line to log; stop never throws.
 */
export function startMeter(opts: MeterOptions): ResourceMeter {
  const readCurrent = opts.readCurrent ?? ((root: string) => readMemory(root, 'memory.current'));
  const readMachine = opts.readMachine ?? readMachineMemory;
  const child =
    opts.selfCgroup === undefined
      ? Promise.resolve(undefined)
      : readSelfCgroup(opts.root, opts.selfCgroup).catch(() => undefined);
  const largest: { child: number | undefined; root: number | undefined; machine: number | undefined } = {
    child: undefined,
    root: undefined,
    machine: undefined,
  };
  const sample = async () => {
    const [atChild, atRoot, machine] = await Promise.all([
      child.then((dir) => (dir === undefined ? undefined : readCurrent(dir))).catch(() => undefined),
      readCurrent(opts.root).catch(() => undefined),
      opts.meminfo === undefined ? undefined : readMachine(opts.meminfo).catch(() => undefined),
    ]);
    largest.child = larger(largest.child, atChild);
    largest.root = larger(largest.root, atRoot);
    largest.machine = larger(largest.machine, machine);
  };
  void sample();
  const timer = setInterval(() => void sample(), opts.sampleMs ?? memorySampleMs);
  timer.unref();

  let stopped: Promise<Usage> | undefined;
  const measure = async (): Promise<Usage> => {
    clearInterval(timer);
    const usage: Usage = {};
    const dir = await child;
    try {
      const [start, end, childPeak, rootPeak] = await Promise.all([
        read(opts.startFile).then((t) => (t === undefined ? undefined : count(t))),
        readCpuUsec(opts.root),
        dir === undefined ? undefined : readMemory(dir, 'memory.peak'),
        readMemory(opts.root, 'memory.peak'),
        sample(),
      ]);
      if (start !== undefined && end !== undefined && end >= start) usage.cpu_seconds = (end - start) / 1e6;
      const cgroup = childPeak ?? largest.child ?? rootPeak ?? largest.root;
      if (cgroup !== undefined) {
        usage.peak_memory_bytes = cgroup;
        usage.memory_source = 'cgroup';
      } else if (largest.machine !== undefined) {
        usage.peak_memory_bytes = largest.machine;
        usage.memory_source = 'machine';
      }
    } catch {
      // Every read above already turns a failure into undefined; this keeps stop from ever throwing.
    }
    const missing = [
      ...(usage.cpu_seconds === undefined ? [`CPU time (${join(opts.root, 'cpu.stat')})`] : []),
      ...(usage.peak_memory_bytes === undefined
        ? [`peak memory (${[dir, opts.root, opts.meminfo].filter((p) => p !== undefined).join(', ')})`]
        : []),
    ];
    if (missing.length > 0) {
      (opts.log ?? stderrLine)(`goal-agent run: no counter could be read for ${missing.join(' or ')}`);
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

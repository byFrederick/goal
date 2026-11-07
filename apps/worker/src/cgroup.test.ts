import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { readCpuUsec, readMachineMemory, readMemory, readSelfCgroup, saveCpuStart, startMeter } from './cgroup.js';

let dir: string;
let root: string;
let startFile: string;

beforeEach(async () => {
  dir = await mkdtemp(join(tmpdir(), 'goal-cgroup-'));
  root = join(dir, 'cgroup');
  startFile = join(dir, 'goal', 'cpu-start-usec');
  await mkdir(root);
});

afterEach(async () => {
  vi.useRealTimers();
  await rm(dir, { recursive: true, force: true });
});

/** Writes a cgroup v2 counter file under the fixture root. */
const counter = (file: string, text: string) => writeFile(join(root, file), text);
const cpuStat = (usec: number | string) =>
  counter('cpu.stat', `usage_usec ${usec}\nuser_usec 1\nsystem_usec 2\nnr_periods 0\n`);

describe('cgroup counters', () => {
  it('reads usage_usec from cpu.stat, memory.peak and memory.current', async () => {
    await cpuStat(2_500_000);
    await counter('memory.peak', '734003200\n');
    await counter('memory.current', '104857600\n');

    expect(await readCpuUsec(root)).toBe(2_500_000);
    expect(await readMemory(root, 'memory.peak')).toBe(734_003_200);
    expect(await readMemory(root, 'memory.current')).toBe(104_857_600);
  });

  it('reads nothing from missing files or values that do not parse', async () => {
    expect(await readCpuUsec(root)).toBeUndefined();
    expect(await readMemory(root, 'memory.peak')).toBeUndefined();

    await counter('cpu.stat', 'user_usec 1\n');
    await counter('memory.peak', 'max\n');
    expect(await readCpuUsec(root)).toBeUndefined();
    expect(await readMemory(root, 'memory.peak')).toBeUndefined();

    await cpuStat('-5');
    expect(await readCpuUsec(root)).toBeUndefined();
  });
});

describe('saveCpuStart and startMeter', () => {
  it('reports the CPU time since prepare and memory.peak when the v2 counters are there', async () => {
    const log = vi.fn();
    await cpuStat(1_000_000);
    await saveCpuStart(root, startFile, log);
    expect(await readFile(startFile, 'utf8')).toBe('1000000');

    const meter = startMeter({ root, startFile, log });
    await cpuStat(4_250_000);
    await counter('memory.peak', '536870912\n');
    await counter('memory.current', '1024\n');

    expect(await meter.stop()).toEqual({ cpu_seconds: 3.25, peak_memory_bytes: 536_870_912, memory_source: 'cgroup' });
    expect(log).not.toHaveBeenCalled();
  });

  it('reports the largest sampled memory.current where memory.peak is missing, and stops sampling', async () => {
    vi.useFakeTimers();
    const log = vi.fn();
    await cpuStat(0);
    await saveCpuStart(root, startFile, log);
    await counter('memory.current', '100\n');

    // The reads are real file I/O, which fake timers do not wait for: keep the last one to await it.
    let lastRead: Promise<unknown> = Promise.resolve();
    const readCurrent = (r: string) => {
      const read = readMemory(r, 'memory.current');
      lastRead = read;
      return read;
    };
    const meter = startMeter({ root, startFile, sampleMs: 5_000, log, readCurrent });
    await lastRead;
    await counter('memory.current', '900\n');
    await vi.advanceTimersByTimeAsync(5_000);
    await lastRead;
    await counter('memory.current', '300\n');
    await vi.advanceTimersByTimeAsync(5_000);
    await lastRead;
    await cpuStat(500_000);

    expect(await meter.stop()).toEqual({ cpu_seconds: 0.5, peak_memory_bytes: 900, memory_source: 'cgroup' });
    expect(vi.getTimerCount()).toBe(0);
    // stop is the same answer every time: the end is read once.
    await counter('memory.current', '5000\n');
    expect(await meter.stop()).toEqual({ cpu_seconds: 0.5, peak_memory_bytes: 900, memory_source: 'cgroup' });
    expect(log).not.toHaveBeenCalled();
  });

  it('reports a reading taken at the end when no sample saw more', async () => {
    await cpuStat(0);
    await saveCpuStart(root, startFile);
    const meter = startMeter({ root, startFile, sampleMs: 60_000 });
    await counter('memory.current', '4096\n');

    expect(await meter.stop()).toMatchObject({ peak_memory_bytes: 4096, memory_source: 'cgroup' });
  });

  it('leaves both fields out, without throwing, when the files are missing or unreadable', async () => {
    const log = vi.fn();
    // A start file from an earlier prepare is removed when the counter can't be read now.
    await mkdir(join(dir, 'goal'));
    await writeFile(startFile, '42');
    await saveCpuStart(root, startFile, log);
    await expect(readFile(startFile, 'utf8')).rejects.toThrow();
    expect(log).toHaveBeenCalledTimes(1);

    const meter = startMeter({ root, startFile, log });
    expect(await meter.stop()).toEqual({});
    expect(log).toHaveBeenCalledTimes(2);

    // Directories where the counters should be: present but unreadable as files.
    await mkdir(join(root, 'cpu.stat'));
    await mkdir(join(root, 'memory.peak'));
    await mkdir(join(root, 'memory.current'));
    await writeFile(startFile, 'not a number');
    expect(await startMeter({ root, startFile, log: () => {} }).stop()).toEqual({});
    await saveCpuStart(root, startFile, () => {});
  });

  it("leaves cpu_seconds out without prepare's reading, or when the counter went backwards", async () => {
    await cpuStat(2_000_000);
    await counter('memory.peak', '2048\n');
    expect(await startMeter({ root, startFile, log: () => {} }).stop()).toEqual({
      peak_memory_bytes: 2048,
      memory_source: 'cgroup',
    });

    await mkdir(join(dir, 'goal'));
    await writeFile(startFile, '9000000');
    expect(await startMeter({ root, startFile, log: () => {} }).stop()).toEqual({
      peak_memory_bytes: 2048,
      memory_source: 'cgroup',
    });
  });
});

describe('startMeter memory sources', () => {
  let selfCgroup: string;
  let meminfo: string;

  beforeEach(() => {
    selfCgroup = join(dir, 'proc-self-cgroup');
    meminfo = join(dir, 'proc-meminfo');
  });

  /** A /proc/meminfo with MemTotal and MemAvailable in kB, as the kernel writes it. */
  const machine = (totalKb: number, availableKb: number) =>
    writeFile(meminfo, `MemTotal:       ${totalKb} kB\nMemFree:        1 kB\nMemAvailable:   ${availableKb} kB\n`);

  /** The box as Cloudflare runs it: the root has cpu.stat only, the worker's cgroup the memory files. */
  const childCgroup = async () => {
    await writeFile(selfCgroup, '0::/cloudchamber\n');
    await mkdir(join(root, 'cloudchamber'));
    return join(root, 'cloudchamber');
  };

  it("reads the worker's own cgroup from the 0:: line, and nothing for the root or a missing line", async () => {
    await writeFile(selfCgroup, '1:name=systemd:/x\n0::/cloudchamber\n');
    expect(await readSelfCgroup(root, selfCgroup)).toBe(join(root, 'cloudchamber'));
    await writeFile(selfCgroup, '0::/\n');
    expect(await readSelfCgroup(root, selfCgroup)).toBeUndefined();
    await writeFile(selfCgroup, '12:memory:/docker/abc\n');
    expect(await readSelfCgroup(root, selfCgroup)).toBeUndefined();
    expect(await readSelfCgroup(root, join(dir, 'missing'))).toBeUndefined();
  });

  it('reads MemTotal less MemAvailable in bytes, and nothing when either is missing', async () => {
    await machine(8_338_056, 7_811_416);
    expect(await readMachineMemory(meminfo)).toBe((8_338_056 - 7_811_416) * 1024);
    await writeFile(meminfo, 'MemTotal:       100 kB\n');
    expect(await readMachineMemory(meminfo)).toBeUndefined();
    expect(await readMachineMemory(join(dir, 'missing'))).toBeUndefined();
  });

  it("reports the worker's own cgroup's memory.peak before the machine's memory", async () => {
    const log = vi.fn();
    await cpuStat(0);
    await saveCpuStart(root, startFile, log);
    const child = await childCgroup();
    await writeFile(join(child, 'memory.peak'), '1654448128\n');
    await writeFile(join(child, 'memory.current'), '1024\n');
    await machine(8_000_000, 1_000_000);

    const meter = startMeter({ root, selfCgroup, meminfo, startFile, log });
    await cpuStat(2_000_000);

    expect(await meter.stop()).toEqual({ cpu_seconds: 2, peak_memory_bytes: 1_654_448_128, memory_source: 'cgroup' });
    expect(log).not.toHaveBeenCalled();
  });

  it("reports the largest memory.current of the worker's own cgroup where it has no memory.peak", async () => {
    const child = await childCgroup();
    await writeFile(join(child, 'memory.current'), '4096\n');
    await counter('memory.peak', '99999999\n');

    const meter = startMeter({ root, selfCgroup, meminfo, startFile, sampleMs: 60_000, log: () => {} });
    expect(await meter.stop()).toMatchObject({ peak_memory_bytes: 4096, memory_source: 'cgroup' });
  });

  it('reports the largest machine memory in use where the root has cpu.stat but no memory files', async () => {
    vi.useFakeTimers();
    const log = vi.fn();
    await cpuStat(0);
    await saveCpuStart(root, startFile, log);
    await writeFile(selfCgroup, '0::/\n');
    await machine(1000, 900);

    let lastRead: Promise<unknown> = Promise.resolve();
    const readMachine = (file: string) => {
      const read = readMachineMemory(file);
      lastRead = read;
      return read;
    };
    const meter = startMeter({ root, selfCgroup, meminfo, startFile, sampleMs: 5_000, log, readMachine });
    await lastRead;
    await machine(1000, 200);
    await vi.advanceTimersByTimeAsync(5_000);
    await lastRead;
    await machine(1000, 600);
    await vi.advanceTimersByTimeAsync(5_000);
    await lastRead;
    await cpuStat(1_000_000);

    expect(await meter.stop()).toEqual({ cpu_seconds: 1, peak_memory_bytes: 800 * 1024, memory_source: 'machine' });
    expect(vi.getTimerCount()).toBe(0);
    expect(log).not.toHaveBeenCalled();
  });

  it('reports the machine memory read at stop when only /proc/meminfo exists', async () => {
    const log = vi.fn();
    await rm(root, { recursive: true });
    const meter = startMeter({ root, selfCgroup, meminfo, startFile, sampleMs: 60_000, log });
    await machine(2048, 1024);

    expect(await meter.stop()).toEqual({ peak_memory_bytes: 1024 * 1024, memory_source: 'machine' });
    // CPU time can't be read, and that is the one line.
    expect(log).toHaveBeenCalledTimes(1);
    expect(log.mock.calls[0]?.[0]).not.toContain('peak memory');
  });

  it('logs exactly one line, and never throws, when no memory source can be read', async () => {
    const log = vi.fn();
    await rm(root, { recursive: true });
    await writeFile(selfCgroup, 'not a cgroup file');
    await mkdir(meminfo);

    const meter = startMeter({ root, selfCgroup, meminfo, startFile, log });
    const usage = await meter.stop();

    expect(usage).toEqual({});
    expect(usage).not.toHaveProperty('memory_source');
    expect(log).toHaveBeenCalledTimes(1);
    expect(log.mock.calls[0]?.[0]).toContain('peak memory');
    await meter.stop();
    expect(log).toHaveBeenCalledTimes(1);
  });
});

import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { readCpuUsec, readMemory, saveCpuStart, startMeter } from './cgroup.js';

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

    expect(await meter.stop()).toEqual({ cpu_seconds: 3.25, peak_memory_bytes: 536_870_912 });
    expect(log).not.toHaveBeenCalled();
  });

  it('reports the largest sampled memory.current where memory.peak is missing, and stops sampling', async () => {
    vi.useFakeTimers();
    const log = vi.fn();
    await cpuStat(0);
    await saveCpuStart(root, startFile, log);
    await counter('memory.current', '100\n');

    const meter = startMeter({ root, startFile, sampleMs: 5_000, log });
    await counter('memory.current', '900\n');
    await vi.advanceTimersByTimeAsync(5_000);
    await counter('memory.current', '300\n');
    await vi.advanceTimersByTimeAsync(5_000);
    await cpuStat(500_000);

    expect(await meter.stop()).toEqual({ cpu_seconds: 0.5, peak_memory_bytes: 900 });
    expect(vi.getTimerCount()).toBe(0);
    // stop is the same answer every time: the end is read once.
    await counter('memory.current', '5000\n');
    expect(await meter.stop()).toEqual({ cpu_seconds: 0.5, peak_memory_bytes: 900 });
    expect(log).not.toHaveBeenCalled();
  });

  it('reports a reading taken at the end when no sample saw more', async () => {
    await cpuStat(0);
    await saveCpuStart(root, startFile);
    const meter = startMeter({ root, startFile, sampleMs: 60_000 });
    await counter('memory.current', '4096\n');

    expect(await meter.stop()).toMatchObject({ peak_memory_bytes: 4096 });
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
    expect(await startMeter({ root, startFile, log: () => {} }).stop()).toEqual({ peak_memory_bytes: 2048 });

    await mkdir(join(dir, 'goal'));
    await writeFile(startFile, '9000000');
    expect(await startMeter({ root, startFile, log: () => {} }).stop()).toEqual({ peak_memory_bytes: 2048 });
  });
});

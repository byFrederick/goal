// The sandbox API (api/sandbox.yaml): one Durable Object per run holds the run's box, a container
// of the `durable_object` scheduling policy started from one of the Worker's named images, and an
// index lists the boxes for goald's reaper. The box's environment comes only from goald's request.

import { DurableObject } from 'cloudflare:workers';
import { type Env, hosts, read } from './env.js';
import { type Base, baseFile, entrypoint, messageOf, parseBase, type Running, stream, wrapped } from './exec.js';
import { outbound } from './services.js';

/** The instance type of each size: the smallest workable is standard-1 (spec 4 §4.6). */
const instances = { small: 'standard-2', medium: 'standard-3', large: 'standard-4' } as const;
type Size = keyof typeof instances;

/** The longest a container may stay up while its Durable Object is idle (the runtime's maximum). */
const maxIdleMs = 6 * 60 * 60_000;

/** How long a new box may take until its entrypoint has written its base. */
const startMs = 5 * 60_000;

const routes = /^\/v1\/sandboxes(?:\/([A-Za-z0-9-]{1,64})(?:\/execs(?:\/([0-9a-f]{1,64}))?)?)?$/;
const variableName = /^[A-Za-z_][A-Za-z0-9_]*$/;

/** sandboxApi answers goald's cloudflare adapter: bearer GOAL_SANDBOX_KEY, then the run's object. */
export async function sandboxApi(request: Request, env: Env): Promise<Response> {
  const key = await read(env.GOAL_SANDBOX_KEY);
  if (!key) return problem(503, 'the Worker has no GOAL_SANDBOX_KEY');
  if (!(await bearerIs(request, key)))
    return problem(401, 'the sandbox API needs Authorization: Bearer GOAL_SANDBOX_KEY');

  const route = routes.exec(new URL(request.url).pathname);
  if (!route) return problem(404, 'no such path');

  const run = route[1];
  if (run === undefined) {
    if (request.method !== 'GET') return problem(405, 'GET only');
    return Response.json({ sandboxes: await index(env).list() });
  }
  return env.BOXES.get(env.BOXES.idFromName(run)).fetch(request);
}

/** The run's box as its Durable Object keeps it. */
interface Box {
  run: string;
  org: string;
  factory: string;
  item: string;
  station: string;
  agent: string;
  image: string;
  size: Size;
  env: Record<string, string>;
  /** Unix milliseconds. */
  deadline: number;
  /** The image's environment and working directory, read once the container runs. */
  base?: Base;
}

/** RunBox is one run's box. */
export class RunBox extends DurableObject<Env> {
  /** The commands running now, by the id their exec request gave. */
  private readonly running = new Map<string, Running>();

  constructor(ctx: DurableObjectState, env: Env) {
    super(ctx, env);
    // A Durable Object that restarted (a deploy) has no idle timeout: give its box one again.
    void ctx.blockConcurrencyWhile(async () => {
      const box = await this.box();
      if (box && this.container().running) await this.keepRunning(box);
    });
  }

  async fetch(request: Request): Promise<Response> {
    const route = routes.exec(new URL(request.url).pathname);
    const run = route?.[1];
    if (!route || run === undefined) return problem(404, 'no such path');
    const exec = route[2];
    const execs = new URL(request.url).pathname.endsWith('/execs');

    try {
      switch (true) {
        case exec !== undefined && request.method === 'DELETE':
          this.running.get(exec)?.kill();
          return new Response(null, { status: 204 });
        case execs && request.method === 'POST':
          return await this.exec(await request.json());
        case !execs && exec === undefined && request.method === 'PUT':
          return await this.create(run, await request.json());
        case !execs && exec === undefined && request.method === 'DELETE':
          await this.destroy();
          return new Response(null, { status: 204 });
        default:
          return problem(405, 'no such method');
      }
    } catch (error) {
      if (error instanceof Refused) return problem(error.status, error.message);
      return problem(502, messageOf(error));
    }
  }

  /** The alarm at the box's deadline destroys it, whether or not a goald is left to. */
  async alarm(): Promise<void> {
    console.log(JSON.stringify({ msg: 'sandbox past its deadline', run: (await this.box())?.run }));
    await this.destroy();
  }

  /** container is the Durable Object's container; tests give a stand-in. */
  protected container(): Container {
    const c = this.ctx.container;
    if (!c) throw new Error('this Durable Object has no container (wrangler.jsonc containers)');
    return c;
  }

  private async create(run: string, body: unknown): Promise<Response> {
    const existing = await this.box();
    if (existing) {
      if (!this.container().running) await this.start(existing);
      return Response.json(entry(existing), { status: 200 });
    }

    const box = specOf(run, body, this.container().images);
    await this.ctx.storage.put('box', box);
    // Listed before it starts, so no box runs that the reaper cannot find.
    await index(this.env).add(entry(box));
    try {
      await this.start(box);
    } catch (error) {
      await this.destroy();
      throw error;
    }
    console.log(JSON.stringify({ msg: 'sandbox created', run, size: box.size }));
    return Response.json(entry(box), { status: 201 });
  }

  /** start starts the box's container, arms its deadline and reads its base. */
  private async start(box: Box): Promise<void> {
    const c = this.container();
    // An image the deploy removed since the box's first start.
    const image = Object.hasOwn(c.images, box.image) ? c.images[box.image] : undefined;
    if (image === undefined) {
      throw new Refused(400, `image ${JSON.stringify(box.image)} is not one of the Worker's images`);
    }
    if (!c.running) {
      c.start({
        image,
        instance: instances[box.size],
        enableInternet: true,
        entrypoint: entrypoint((box.deadline - Date.now()) / 1000),
        labels: { run: box.run, org: box.org },
      });
    }
    await this.ctx.storage.setAlarm(box.deadline);
    await this.keepRunning(box);
    box.base = await readBase(c);
    await this.ctx.storage.put('box', box);
  }

  /** keepRunning keeps the container up between commands, and gives it the gateway. */
  private async keepRunning(box: Box): Promise<void> {
    const c = this.container();
    await c.setInactivityTimeout(Math.min(maxIdleMs, Math.max(1, box.deadline - Date.now())));
    await c.interceptOutboundHttp(hosts.gateway, outbound(this.ctx).BoxOutbound({ props: {} }));
  }

  private async exec(body: unknown): Promise<Response> {
    const box = await this.box();
    if (!box) throw new Refused(404, 'no sandbox for this run');
    const c = this.container();
    if (!c.running || !box.base) throw new Refused(409, 'the sandbox is not running');

    const e = execOf(body);
    if (this.running.has(e.id)) throw new Refused(409, `exec ${e.id} exists`);

    const process = await c.exec(wrapped(e.argv, e.timeout), {
      env: { ...box.base.env, ...box.env, ...e.env },
      cwd: e.dir || box.base.dir,
    });
    const { body: out, running } = stream(process, e.timeout, () => this.running.delete(e.id));
    this.running.set(e.id, running);
    return new Response(out, { headers: { 'content-type': 'application/x-ndjson' } });
  }

  /** destroy ends the box and every command in it, and forgets it. */
  private async destroy(): Promise<void> {
    const box = await this.box();
    const c = this.container();
    if (c.running) await c.destroy();
    for (const r of this.running.values()) r.end('the sandbox was deleted');
    this.running.clear();
    if (box) await index(this.env).remove(box.run);
    await this.ctx.storage.deleteAlarm();
    await this.ctx.storage.deleteAll();
    if (box) console.log(JSON.stringify({ msg: 'sandbox deleted', run: box.run }));
  }

  private box(): Promise<Box | undefined> {
    return this.ctx.storage.get<Box>('box');
  }
}

/** BoxIndex lists every box the Worker holds, for goald's reaper (Sandboxes.List). */
export class BoxIndex extends DurableObject<Env> {
  async add(e: Entry): Promise<void> {
    await this.ctx.storage.put(`box:${e.run}`, e);
  }

  async remove(run: string): Promise<void> {
    await this.ctx.storage.delete(`box:${run}`);
  }

  async list(): Promise<Entry[]> {
    return [...(await this.ctx.storage.list<Entry>({ prefix: 'box:' })).values()];
  }
}

function index(env: Env): DurableObjectStub<BoxIndex> {
  return env.BOX_INDEX.get(env.BOX_INDEX.idFromName('index'));
}

interface Entry {
  run: string;
  org: string;
  deadline: string;
}

function entry(box: Box): Entry {
  return { run: box.run, org: box.org, deadline: new Date(box.deadline).toISOString() };
}

/** readBase waits for the entrypoint's base file, for at most startMs. */
async function readBase(c: Container): Promise<Base> {
  const until = Date.now() + startMs;
  let last = 'no answer';
  for (;;) {
    try {
      const p = await c.exec(['cat', baseFile]);
      const out = await p.output();
      if (out.exitCode === 0) return parseBase(new TextDecoder().decode(out.stdout));
      last = new TextDecoder().decode(out.stderr).trim() || `cat exited ${out.exitCode}`;
    } catch (error) {
      last = messageOf(error);
    }
    if (Date.now() > until) throw new Error(`the sandbox did not start within ${startMs / 1000}s: ${last}`);
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
}

/** A request the API refuses, with its status. */
class Refused extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

function specOf(run: string, body: unknown, images: Record<string, string>): Box {
  const b = object(body, 'the sandbox');
  const size = text(b, 'size', 16);
  if (!Object.hasOwn(instances, size)) throw new Refused(400, 'size is not small, medium or large');
  const image = text(b, 'image', 128);
  if (!Object.hasOwn(images, image)) {
    throw new Refused(400, `image ${JSON.stringify(image)} is not one of the Worker's images`);
  }
  const deadline = Date.parse(text(b, 'deadline', 64));
  if (!Number.isFinite(deadline) || deadline <= Date.now())
    throw new Refused(400, 'deadline is not a time in the future');
  const org = text(b, 'org', 64);
  if (org === '') throw new Refused(400, 'org is required');

  return {
    run,
    org,
    factory: text(b, 'factory', 64),
    item: text(b, 'item', 64),
    station: text(b, 'station', 128),
    agent: text(b, 'agent', 128),
    image,
    size: size as Size,
    env: envOf(b.env, 'env'),
    deadline,
  };
}

interface ExecRequest {
  id: string;
  argv: string[];
  env: Record<string, string>;
  dir: string;
  timeout: number;
}

function execOf(body: unknown): ExecRequest {
  const b = object(body, 'the exec');
  const id = text(b, 'id', 64);
  if (!/^[0-9a-f]{1,64}$/.test(id)) throw new Refused(400, 'id is not 1 to 64 hex digits');
  const argv = b.argv;
  if (!Array.isArray(argv) || argv.length === 0 || !argv.every((a) => typeof a === 'string' && !a.includes('\0'))) {
    throw new Refused(400, 'argv is not a list of strings with a command');
  }
  const timeout = b.timeout_s ?? 0;
  if (typeof timeout !== 'number' || !Number.isInteger(timeout) || timeout < 0) {
    throw new Refused(400, 'timeout_s is not a whole number of seconds');
  }
  const dir = b.dir ?? '';
  if (typeof dir !== 'string' || dir.includes('\0')) throw new Refused(400, 'dir is not a path');
  return { id, argv: argv as string[], env: envOf(b.env, 'env'), dir, timeout };
}

function object(value: unknown, what: string): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value))
    throw new Refused(400, `${what} is not an object`);
  return value as Record<string, unknown>;
}

function text(b: Record<string, unknown>, name: string, max: number): string {
  const v = b[name] ?? '';
  if (typeof v !== 'string' || v.length > max)
    throw new Refused(400, `${name} is not a string of at most ${max} characters`);
  return v;
}

function envOf(value: unknown, what: string): Record<string, string> {
  if (value === undefined || value === null) return {};
  const env = object(value, what);
  for (const [k, v] of Object.entries(env)) {
    if (!variableName.test(k) || typeof v !== 'string' || v.includes('\0')) {
      throw new Refused(400, `${what} has a variable that is not NAME=text`);
    }
  }
  return env as Record<string, string>;
}

/** bearerIs compares the request's bearer with key in constant time. */
async function bearerIs(request: Request, key: string): Promise<boolean> {
  const given = request.headers.get('authorization')?.match(/^Bearer (.+)$/)?.[1];
  if (given === undefined) return false;
  const digest = async (s: string) => crypto.subtle.digest('SHA-256', new TextEncoder().encode(s));
  return crypto.subtle.timingSafeEqual(await digest(given), await digest(key));
}

function problem(status: number, error: string): Response {
  return Response.json({ error }, { status });
}

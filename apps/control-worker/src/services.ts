// goald and the auth service: one container each, behind a Durable Object of their own (the
// `default` scheduling policy: wrangler builds their images and sets their instance types).

import { DurableObject } from 'cloudflare:workers';
import { authEnv, type Env, goaldEnv, hosts, ports } from './env.js';

/** How long a service may take from its start until its health check answers. */
const bootMs = 120_000;

/** How long one health check may take before it is sent again: a hung one must not hold a boot. */
const healthMs = 5_000;

/** How often goald's alarm wakes its Durable Object (Goald.alarm). */
export const heartbeatMs = 10_000;

/** Services sit in eastern North America, near the database (spec 4 §5.12). */
const near: DurableObjectLocationHint = 'enam';

/** The outbound handlers the services' and boxes' containers get (index.ts exports them). */
export interface Outbound {
  GoaldOutbound(options: { props: Record<string, never> }): Fetcher;
  BoxOutbound(options: { props: Record<string, never> }): Fetcher;
}

/** outbound is ctx.exports with the Worker's own entrypoints. */
export function outbound(ctx: DurableObjectState): Outbound {
  return ctx.exports as unknown as Outbound;
}

/**
 * A long-running service in a container: started on its first request, with only its own
 * environment, and kept running until its Durable Object has been idle for idleMs.
 */
abstract class Service extends DurableObject<Env> {
  /** service names the container in the Worker's log lines. */
  protected abstract service(): string;
  protected abstract readonly port: number;
  protected abstract readonly health: string;
  /**
   * idleMs is how long the container outlives its idle Durable Object. It and service are methods,
   * not fields, because the constructor calls them before a subclass's fields exist.
   */
  protected abstract idleMs(): number;
  protected abstract environment(): Promise<Record<string, string>>;
  /** intercept registers the container's outbound handlers. */
  protected abstract intercept(container: Container): Promise<void>;

  /** booting is the boot in flight, or the last one that finished (booted then says so). */
  private booting: Promise<void> | undefined;
  private booted = false;
  /** watching says whether a monitor() call waits for the running container to stop. */
  private watching = false;

  /**
   * A Durable Object that Cloudflare restarts, with or without a deploy, starts without the
   * inactivity timeout, and Cloudflare stops a container that has none shortly after its object
   * goes inactive. So a new instance that finds its container running sets the timeout again
   * before it takes any call, and watches the container (2026-11-05 in cloudflare-operations.md).
   */
  constructor(ctx: DurableObjectState, env: Env) {
    super(ctx, env);
    const c = this.container();
    if (!c.running) return;
    console.log(JSON.stringify({ msg: 'container found running', service: this.service() }));
    void ctx.blockConcurrencyWhile(async () => {
      // A throw here would reset the object again; the next boot() sets the timeout instead.
      await c.setInactivityTimeout(this.idleMs()).catch(() => {});
    });
    this.watch(c);
  }

  /**
   * forward sends request to the container's port (its main port by default). A container is
   * reached over HTTP only, so an https request keeps its host, path and query, and the scheme it
   * arrived with goes in X-Forwarded-Proto. A GET or HEAD that fails because the container stopped
   * under it (a deploy's rollover stops the old one) is sent once more after a new boot.
   */
  async forward(request: Request, port?: number): Promise<Response> {
    await this.ensure();
    const url = new URL(request.url);
    const headers = new Headers(request.headers);
    headers.set('x-forwarded-proto', url.protocol.slice(0, -1));
    url.protocol = 'http:';
    const send = () =>
      this.container()
        .getTcpPort(port ?? this.port)
        .fetch(new Request(url, new Request(request, { headers })));
    try {
      return await send();
    } catch (error) {
      if (!idempotent(request) || this.container().running) throw error;
      await this.ensure();
      return send();
    }
  }

  /**
   * ensure starts the container unless it runs, and waits until it answers its health check.
   * Callers that arrive during a boot wait for that boot: the container is not running until
   * boot() has read the settings and started it, so not running alone does not mean stopped.
   */
  ensure(): Promise<void> {
    if (this.booted && !this.container().running) this.booting = undefined;
    if (!this.booting) {
      this.booted = false;
      const boot: Promise<void> = this.boot().then(
        () => {
          if (this.booting === boot) this.booted = true;
        },
        (error: unknown) => {
          if (this.booting === boot) this.booting = undefined;
          throw error;
        },
      );
      this.booting = boot;
    }
    return this.booting;
  }

  /** container is the Durable Object's container; tests give a stand-in. */
  protected container(): Container {
    const c = this.ctx.container;
    if (!c) throw new Error('this Durable Object has no container (wrangler.jsonc containers)');
    return c;
  }

  /**
   * boot starts the container and waits for its health check, for at most bootMs. Cloudflare
   * refuses a start while it has no instance for the Durable Object yet ("try again later", seen
   * during a deploy's rollover), so a failed start is tried again until then.
   */
  private async boot(): Promise<void> {
    const until = Date.now() + bootMs;
    const c = this.container();
    for (;;) {
      try {
        if (!c.running) {
          c.start({ env: await this.environment(), enableInternet: true });
          console.log(JSON.stringify({ msg: 'container started', service: this.service() }));
        }
        this.watch(c);
        await c.setInactivityTimeout(this.idleMs());
        await this.intercept(c);
        break;
      } catch (error) {
        if (Date.now() > until) throw error;
        await new Promise((resolve) => setTimeout(resolve, 500));
      }
    }
    await healthy(c.getTcpPort(this.port), this.health, until);
  }

  /**
   * watch logs one line when the running container stops: its exit code (0 for a clean exit,
   * null when Cloudflare gives none) and the reason. A monitor() call does not survive a restart of
   * the Durable Object, so each new instance that finds the container running watches it again.
   */
  private watch(c: Container): void {
    if (this.watching) return;
    this.watching = true;
    const stopped = (exitCode: number | null, reason: string) => {
      this.watching = false;
      console.warn(JSON.stringify({ msg: 'container stopped', service: this.service(), exitCode, reason }));
    };
    c.monitor().then(
      () => stopped(0, 'exited'),
      (error: unknown) => {
        const code = (error as { exitCode?: unknown } | null)?.exitCode;
        stopped(typeof code === 'number' ? code : null, error instanceof Error ? error.message : String(error));
      },
    );
  }
}

/** goald: every public request, its gateway for the boxes; kept warm by the Worker's cron. */
export class Goald extends Service {
  protected service(): string {
    return 'goald';
  }
  protected readonly port = ports.goald;
  protected readonly health = '/v1/health';
  protected idleMs(): number {
    return 15 * 60_000;
  }

  protected environment(): Promise<Record<string, string>> {
    return goaldEnv(this.env);
  }

  /** armed says whether this instance has made sure the alarm is set. */
  private armed = false;

  /** ensure boots goald like Service.ensure, and sets the alarm once per instance. */
  override async ensure(): Promise<void> {
    await super.ensure();
    if (this.armed) return;
    if ((await this.ctx.storage.getAlarm()) === null) await this.ctx.storage.setAlarm(Date.now() + heartbeatMs);
    this.armed = true;
  }

  /**
   * The cron reaches goald once a minute, but on 2026-11-05 Cloudflare stopped goald's container
   * 26 s and 57 s after it restarted this Durable Object, before the next call built a new
   * instance. An alarm outlives the restart: within heartbeatMs it builds the new instance, whose
   * constructor sets the inactivity timeout again, and it boots goald as the cron does.
   */
  override async alarm(): Promise<void> {
    try {
      await super.ensure();
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      console.error(JSON.stringify({ msg: 'boot failed', service: this.service(), reason }));
    } finally {
      await this.ctx.storage.setAlarm(Date.now() + heartbeatMs);
      this.armed = true;
    }
  }

  protected async intercept(c: Container): Promise<void> {
    const handler = outbound(this.ctx).GoaldOutbound({ props: {} });
    await c.interceptOutboundHttp(hosts.auth, handler);
    await c.interceptOutboundHttp(hosts.sandbox, handler);
  }
}

/** The auth service: only goald's /api/auth proxy reaches it, as auth.internal. */
export class Auth extends Service {
  protected service(): string {
    return 'auth';
  }
  protected readonly port = ports.auth;
  protected readonly health = '/api/auth/ok';
  protected idleMs(): number {
    return 30 * 60_000;
  }

  protected environment(): Promise<Record<string, string>> {
    return authEnv(this.env);
  }

  protected async intercept(): Promise<void> {
    // The auth service calls nothing of the Worker's: mail and the database are on the internet.
  }
}

export function goald(env: Env): DurableObjectStub<Goald> {
  return env.GOALD.get(env.GOALD.idFromName('goald'), { locationHint: near });
}

export function auth(env: Env): DurableObjectStub<Auth> {
  return env.AUTH.get(env.AUTH.idFromName('auth'), { locationHint: near });
}

/** idempotent says whether request can be sent again: a GET or HEAD, which has no body. */
export function idempotent(request: Request): boolean {
  return request.method === 'GET' || request.method === 'HEAD';
}

/**
 * healthy polls path on the container's port until it answers 2xx, until the time until. Each
 * check gets healthMs: a check that hangs is given up and sent again, so the boot still ends by until.
 */
async function healthy(port: Fetcher, path: string, until: number): Promise<void> {
  let last = 'no answer';
  for (;;) {
    try {
      const res = await port.fetch(`http://container${path}`, { signal: AbortSignal.timeout(healthMs) });
      await res.body?.cancel();
      if (res.ok) return;
      last = `HTTP ${res.status}`;
    } catch (error) {
      last = error instanceof Error ? error.message : String(error);
    }
    if (Date.now() > until) throw new Error(`the container did not answer ${path} within ${bootMs / 1000}s: ${last}`);
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
}

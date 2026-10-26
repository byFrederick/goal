// goald and the auth service: one container each, behind a Durable Object of their own (the
// `default` scheduling policy: wrangler builds their images and sets their instance types).

import { DurableObject } from 'cloudflare:workers';
import { authEnv, type Env, goaldEnv, hosts, ports } from './env.js';

/** How long a service may take from its start until its health check answers. */
const bootMs = 120_000;

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
  protected abstract readonly port: number;
  protected abstract readonly health: string;
  protected abstract readonly idleMs: number;
  protected abstract environment(): Promise<Record<string, string>>;
  /** intercept registers the container's outbound handlers. */
  protected abstract intercept(container: Container): Promise<void>;

  private booting: Promise<void> | undefined;

  /**
   * forward sends request to the container's port (its main port by default). A container is
   * reached over HTTP only, so an https request keeps its host, path and query, and the scheme it
   * arrived with goes in X-Forwarded-Proto.
   */
  async forward(request: Request, port?: number): Promise<Response> {
    await this.ensure();
    const url = new URL(request.url);
    const headers = new Headers(request.headers);
    headers.set('x-forwarded-proto', url.protocol.slice(0, -1));
    url.protocol = 'http:';
    return this.container()
      .getTcpPort(port ?? this.port)
      .fetch(new Request(url, new Request(request, { headers })));
  }

  /** ensure starts the container unless it runs, and waits until it answers its health check. */
  ensure(): Promise<void> {
    if (!this.container().running) this.booting = undefined;
    this.booting ??= this.boot().catch((error: unknown) => {
      this.booting = undefined;
      throw error;
    });
    return this.booting;
  }

  /** container is the Durable Object's container; tests give a stand-in. */
  protected container(): Container {
    const c = this.ctx.container;
    if (!c) throw new Error('this Durable Object has no container (wrangler.jsonc containers)');
    return c;
  }

  private async boot(): Promise<void> {
    const c = this.container();
    if (!c.running) c.start({ env: await this.environment(), enableInternet: true });
    await c.setInactivityTimeout(this.idleMs);
    await this.intercept(c);
    await healthy(c.getTcpPort(this.port), this.health);
  }
}

/** goald: every public request, its gateway for the boxes; kept warm by the Worker's cron. */
export class Goald extends Service {
  protected readonly port = ports.goald;
  protected readonly health = '/v1/health';
  protected readonly idleMs = 15 * 60_000;

  protected environment(): Promise<Record<string, string>> {
    return goaldEnv(this.env);
  }

  protected async intercept(c: Container): Promise<void> {
    const handler = outbound(this.ctx).GoaldOutbound({ props: {} });
    await c.interceptOutboundHttp(hosts.auth, handler);
    await c.interceptOutboundHttp(hosts.sandbox, handler);
  }
}

/** The auth service: only goald's /api/auth proxy reaches it, as auth.internal. */
export class Auth extends Service {
  protected readonly port = ports.auth;
  protected readonly health = '/api/auth/ok';
  protected readonly idleMs = 30 * 60_000;

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

/** healthy polls path on the container's port until it answers 2xx, for at most bootMs. */
async function healthy(port: Fetcher, path: string): Promise<void> {
  const until = Date.now() + bootMs;
  let last = 'no answer';
  for (;;) {
    try {
      const res = await port.fetch(`http://container${path}`);
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

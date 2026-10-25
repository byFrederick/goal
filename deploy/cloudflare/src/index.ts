// Goal's control Worker (spec 4 §5.12, ADR 0008): the only Cloudflare-specific code besides the Go
// adapter internal/adapters/cloudflare.
//
//   public https       ─▶ goald's container (every path)
//   goald ─▶ auth.internal    ─▶ the auth service's container
//   goald ─▶ sandbox.internal ─▶ the sandbox API (api/sandbox.yaml): one Durable Object per run
//   box   ─▶ gateway.internal ─▶ goald's gateway listener (/mcp, /run)
//
// The .internal names exist only inside the containers, where the Worker's outbound handlers
// answer them. Everything else a container sends goes to the internet.

import { WorkerEntrypoint } from 'cloudflare:workers';
import { type Env, hosts, ports } from './env.js';
import { sandboxApi } from './sandbox.js';
import { auth, goald } from './services.js';

export { BoxIndex, RunBox } from './sandbox.js';
export { Auth, Goald } from './services.js';

/** GoaldOutbound answers goald's container: the auth service and the sandbox API. */
export class GoaldOutbound extends WorkerEntrypoint<Env> {
  override fetch(request: Request): Promise<Response> {
    switch (new URL(request.url).hostname) {
      case hosts.auth:
        return auth(this.env).forward(request);
      case hosts.sandbox:
        return sandboxApi(request, this.env);
      default:
        return Promise.resolve(new Response('not found', { status: 404 }));
    }
  }
}

/** BoxOutbound answers a box: goald's gateway, which serves /mcp and /run and nothing else. */
export class BoxOutbound extends WorkerEntrypoint<Env> {
  override fetch(request: Request): Promise<Response> {
    if (new URL(request.url).hostname !== hosts.gateway)
      return Promise.resolve(new Response('not found', { status: 404 }));
    return goald(this.env).forward(request, ports.gateway);
  }
}

export default {
  /** Every public request goes to goald, with the client's address as Cloudflare saw it. */
  fetch(request: Request, env: Env): Promise<Response> {
    const headers = new Headers(request.headers);
    headers.delete('x-forwarded-for');
    const client = request.headers.get('cf-connecting-ip');
    if (client) headers.set('x-forwarded-for', client);
    return goald(env).forward(new Request(request, { headers }));
  },

  /** The cron trigger keeps goald running: its engine works whether or not anyone is looking. */
  async scheduled(_controller: ScheduledController, env: Env): Promise<void> {
    await goald(env).ensure();
  },
} satisfies ExportedHandler<Env>;

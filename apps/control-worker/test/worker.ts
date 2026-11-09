// The Worker under Vitest: src/index.ts with each Durable Object's container replaced by a stand-in.
// Their settings take a moment to read, as Secrets Store reads do in production, so a request can
// arrive while a boot is still reading them.

import { RunBox as RealRunBox } from '../src/sandbox.js';
import { Auth as RealAuth, Goald as RealGoald } from '../src/services.js';
import { fakeFor } from './fake.js';

export { BoxIndex, BoxOutbound, default, GoaldOutbound } from '../src/index.js';

/** secretsMs is how long reading a service's settings takes. */
const secretsMs = 5;

export class Goald extends RealGoald {
  protected override container(): Container {
    return fakeFor(this.ctx.id) as unknown as Container;
  }

  protected override bootMs(): number {
    return fakeFor(this.ctx.id).bootMs ?? super.bootMs();
  }

  protected override async environment(): Promise<Record<string, string>> {
    await scheduler.wait(secretsMs);
    return super.environment();
  }
}

export class Auth extends RealAuth {
  protected override container(): Container {
    return fakeFor(this.ctx.id) as unknown as Container;
  }

  protected override async environment(): Promise<Record<string, string>> {
    await scheduler.wait(secretsMs);
    return super.environment();
  }
}

export class RunBox extends RealRunBox {
  protected override container(): Container {
    return fakeFor(this.ctx.id) as unknown as Container;
  }
}

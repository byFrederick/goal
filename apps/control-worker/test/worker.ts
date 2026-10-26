// The Worker under Vitest: src/index.ts with each Durable Object's container replaced by a stand-in.

import { RunBox as RealRunBox } from '../src/sandbox.js';
import { Auth as RealAuth, Goald as RealGoald } from '../src/services.js';
import { fakeFor } from './fake.js';

export { BoxIndex, BoxOutbound, default, GoaldOutbound } from '../src/index.js';

export class Goald extends RealGoald {
  protected override container(): Container {
    return fakeFor(this.ctx.id) as unknown as Container;
  }
}

export class Auth extends RealAuth {
  protected override container(): Container {
    return fakeFor(this.ctx.id) as unknown as Container;
  }
}

export class RunBox extends RealRunBox {
  protected override container(): Container {
    return fakeFor(this.ctx.id) as unknown as Container;
  }
}

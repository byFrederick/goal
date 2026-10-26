// The sandbox API on its own, for sandboxtest (task cloudflare:sandboxtest): a Worker whose public
// fetch is the sandbox API behind its key, with real boxes (wrangler dev runs them in Docker). The
// control Worker itself never serves the API publicly: only goald's container reaches it.

import type { Env } from '../../src/env.js';
import { sandboxApi } from '../../src/sandbox.js';

export { BoxOutbound } from '../../src/index.js';
export { BoxIndex, RunBox } from '../../src/sandbox.js';

export default {
  fetch(request: Request, env: Env): Promise<Response> {
    return sandboxApi(request, env);
  },
} satisfies ExportedHandler<Env>;

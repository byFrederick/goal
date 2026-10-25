// The bindings and exports of the Worker the tests run (cloudflare:test's env, cloudflare:workers' exports).
declare namespace Cloudflare {
  interface Env {
    GOALD: DurableObjectNamespace<import('../src/services.js').Goald>;
    AUTH: DurableObjectNamespace<import('../src/services.js').Auth>;
    BOXES: DurableObjectNamespace<import('../src/sandbox.js').RunBox>;
    BOX_INDEX: DurableObjectNamespace<import('../src/sandbox.js').BoxIndex>;
    [name: string]: unknown;
  }
  interface Exports {
    default: Fetcher;
    GoaldOutbound: Fetcher;
    BoxOutbound: Fetcher;
  }
}

// The Worker's bindings, and which of them each container gets (spec 4 §5.12, §11.1 "Cloudflare
// env"): goald and the auth service each receive exactly their own settings, read from the
// Worker's vars and Secrets Store bindings by name; a run's box receives none of them.

import type { BoxIndex, RunBox } from './sandbox.js';
import type { Auth, Goald } from './services.js';

/** A setting: a plain var, or a Secrets Store secret read when a container starts. */
export type Setting = string | { get(): Promise<string> };

export interface Env {
  GOALD: DurableObjectNamespace<Goald>;
  AUTH: DurableObjectNamespace<Auth>;
  BOXES: DurableObjectNamespace<RunBox>;
  BOX_INDEX: DurableObjectNamespace<BoxIndex>;
  /** The bearer goald's sandbox API calls carry (goald gets the same value as GOAL_SANDBOX_KEY). */
  GOAL_SANDBOX_KEY?: Setting;
  /** Every other setting, under the name the container reads it by. */
  [name: string]: unknown;
}

/** The names the Worker answers inside Cloudflare: never resolvable from the internet. */
export const hosts = {
  /** The auth service, for goald's /api/auth proxy. */
  auth: 'auth.internal',
  /** The sandbox API (contracts/api/sandbox.yaml), for goald's cloudflare adapter. */
  sandbox: 'sandbox.internal',
  /** goald's gateway (/mcp, /run), for the boxes. */
  gateway: 'gateway.internal',
} as const;

/** The containers' ports: goald's API and UI, its gateway listener, the auth service. */
export const ports = { goald: 8080, gateway: 8090, auth: 8081 } as const;

/** What the Worker's topology fixes in goald's environment, whatever its vars say. */
const goaldTopology = {
  GOAL_LISTEN: `0.0.0.0:${ports.goald}`,
  GOAL_GATEWAY_LISTEN: `0.0.0.0:${ports.gateway}`,
  GOAL_GATEWAY_URL: `http://${hosts.gateway}/mcp`,
  GOAL_AUTH_UPSTREAM: `http://${hosts.auth}`,
  GOAL_SANDBOX: 'cloudflare',
  GOAL_SANDBOX_URL: `http://${hosts.sandbox}`,
  // Nothing but this Worker reaches goald's port 8080 (a container has no address of its own on
  // the internet, and boxes reach only the gateway's 8090), and the Worker replaces X-Forwarded-For
  // with Cloudflare's client address: so goald believes every peer, whatever address Cloudflare
  // gives the Worker's requests inside the container, and the auth service's rate limits see each
  // client instead of the Worker (goald refuses GOAL_SANDBOX=cloudflare under session without it).
  GOAL_TRUSTED_PROXIES: '0.0.0.0/0,::/0',
};

/** goald's settings the Worker passes on when it has them (cmd/goald/settings.go reads them). */
const goaldSettings = [
  'GOAL_PUBLIC_URL',
  'GOAL_AUTH',
  'GOAL_AUTH_GOOGLE',
  'GOAL_AUTH_EMAIL',
  'GOAL_DATABASE_URL',
  'GOAL_MIGRATE_DATABASE_URL',
  'GOAL_S3_BUCKET',
  'GOAL_S3_ENDPOINT',
  'GOAL_S3_ACCESS_KEY',
  'GOAL_S3_SECRET_KEY',
  'AWS_REGION',
  'GOAL_GATEWAY_KEY',
  'GOAL_MASTER_KEYS',
  'GOAL_SANDBOX_KEY',
  'GOAL_AUTH_PROXY_SECRET',
  'GOAL_AGENT_IMAGE',
  'GOAL_MAX_RUNS',
  'GOAL_MODELS',
  'GOAL_EMBED_URL',
  'GOAL_EMBED_MODEL',
  'GOAL_EMBED_KEY',
  'GOAL_KB_WORKERS',
  'GOAL_UPLOAD_MAX_BYTES',
  'GOAL_ORG_RETENTION',
  'GOAL_LOG_LEVEL',
  'GOAL_LOG_FORMAT',
] as const;

/** What the topology fixes in the auth service's environment. */
const authTopology = { GOAL_AUTH_LISTEN: `0.0.0.0:${ports.auth}` };

/** The auth service's settings (auth/src/settings.ts). */
const authSettings = [
  'GOAL_PUBLIC_URL',
  'GOAL_AUTH_GOOGLE',
  'GOAL_AUTH_EMAIL',
  'GOAL_AUTH_DATABASE_URL',
  'BETTER_AUTH_SECRET',
  'BETTER_AUTH_SECRETS',
  'GOAL_AUTH_PROXY_SECRET',
  'GOAL_BOOTSTRAP_OWNER_EMAIL',
  'GOAL_SMTP_URL',
  'GOAL_SMTP_FROM',
  'GOOGLE_CLIENT_ID',
  'GOOGLE_CLIENT_SECRET',
] as const;

/** goald's container environment: its settings, then the topology on top. */
export async function goaldEnv(env: Env): Promise<Record<string, string>> {
  return { ...(await settingsOf(goaldSettings, env)), ...goaldTopology };
}

/** The auth service's container environment. */
export async function authEnv(env: Env): Promise<Record<string, string>> {
  return { ...(await settingsOf(authSettings, env)), ...authTopology };
}

/** The value of each named setting the Worker has; names it lacks are left out. */
export async function settingsOf(names: readonly string[], env: Env): Promise<Record<string, string>> {
  const out: Record<string, string> = {};
  for (const name of names) {
    const value = await read(env[name]);
    if (value !== undefined) out[name] = value;
  }
  return out;
}

/** read resolves a setting: a var as it is, a Secrets Store secret by its get(). */
export async function read(setting: unknown): Promise<string | undefined> {
  if (typeof setting === 'string') return setting;
  if (typeof setting === 'object' && setting !== null && 'get' in setting && typeof setting.get === 'function') {
    return String(await setting.get());
  }
  return undefined;
}

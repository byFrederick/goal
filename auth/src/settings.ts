// The auth service's settings, from its environment only (spec 3 §5.6). goal up passes it its own
// variables and nothing of goald's; goald never sees these. Every value is checked at start and
// every problem is reported at once, without the values of secrets.

/** Versioned Better Auth secrets, newest first (BETTER_AUTH_SECRETS="2:new,1:old"). */
export interface VersionedSecret {
  version: number;
  value: string;
}

export interface Settings {
  /**
   * GOAL_AUTH_LISTEN: loopback beside goald, or every interface in a container of its own that
   * only goald reaches (Cloudflare); goald's /api/auth proxy is the only way in.
   */
  listen: { host: string; port: number };
  /** GOAL_PUBLIC_URL, Better Auth's baseURL and only trusted origin: the origin people open. */
  publicURL: string;
  /** GOAL_AUTH_DATABASE_URL: the database as goal_auth. */
  databaseURL: string;
  /** BETTER_AUTH_SECRET: the secret, or the legacy one beside versioned secrets. */
  secret: string | undefined;
  /** BETTER_AUTH_SECRETS: versioned secrets for rotation. */
  secrets: VersionedSecret[] | undefined;
  /** Google sign-in (GOAL_AUTH_GOOGLE=on), with its client. */
  google: { clientId: string; clientSecret: string } | undefined;
  /**
   * GOAL_BOOTSTRAP_OWNER_EMAIL, trimmed and in lower case: the one address that may sign up while
   * no user exists, then becomes platform admin and owner of every organization (spec 3 §8.7).
   */
  bootstrapOwnerEmail: string | undefined;
  /**
   * GOAL_SMTP_URL (it may hold the server's credentials) and GOAL_SMTP_FROM: where mail goes.
   * plaintext: smtp:// may stay unencrypted, as to a loopback host or, with GOAL_SMTP_PLAINTEXT=on,
   * to compose's Mailpit (mailpit, in the stack profile); any other server must offer STARTTLS.
   */
  smtp: { url: string; from: string; plaintext: boolean };
  /**
   * GOAL_AUTH_PROXY_SECRET: goald sends it with every request it forwards (X-Goal-Proxy-Secret),
   * and the service answers nothing else but its health check, so a process that reaches its
   * loopback port directly, without goald's checks and rate-limit address, gets nowhere.
   */
  proxySecret: string;
}

export type Env = Readonly<Record<string, string | undefined>>;

/** SettingsError lists every problem of the environment; none carries a secret's value. */
export class SettingsError extends Error {
  constructor(readonly problems: string[]) {
    super(`invalid settings:\n- ${problems.join('\n- ')}`);
    this.name = 'SettingsError';
  }
}

const defaultListen = '127.0.0.1:8081';
const defaultPublicURL = 'http://127.0.0.1:8080'; // goald's default GOAL_LISTEN
const devAuthPassword = 'goal-auth-dev-only'; // GOAL_PG_AUTH_PASSWORD's documented dev-only default
const minSecretBytes = 32;
const defaultSMTPURL = 'smtp://127.0.0.1:1025'; // Mailpit from compose.yaml
const composeMailpit = 'mailpit'; // its service name in compose.yaml, for the stack profile
const defaultSMTPFrom = 'Goal <goal@localhost>';
const emailPattern = /^[^\s@<>]+@[^\s@<>]+$/;

export function loadSettings(env: Env): Settings {
  const problems: string[] = [];
  const get = (name: string): string | undefined => {
    const v = env[name]?.trim();
    return v === '' ? undefined : v;
  };

  const listen = parseListen(get('GOAL_AUTH_LISTEN') ?? defaultListen, problems);
  const publicURL = parseOrigin('GOAL_PUBLIC_URL', get('GOAL_PUBLIC_URL') ?? defaultPublicURL, problems);
  // Every interface is for a container of the service's own that only goald reaches (Cloudflare,
  // through the control Worker), always a deployment behind https; never this machine's network.
  if (listen && everyInterface(listen.host) && publicURL && !publicURL.startsWith('https:')) {
    problems.push(
      'GOAL_AUTH_LISTEN on every interface needs an https GOAL_PUBLIC_URL: it is for a container of the ' +
        'service’s own that only goald reaches; beside goald on this machine, listen on loopback',
    );
  }

  const betterAuthURL = get('BETTER_AUTH_URL');
  if (betterAuthURL !== undefined && publicURL !== undefined) {
    const same = parseOrigin('BETTER_AUTH_URL', betterAuthURL, problems);
    if (same !== undefined && same !== publicURL) problems.push('BETTER_AUTH_URL must equal GOAL_PUBLIC_URL');
  }

  if (get('BETTER_AUTH_TRUSTED_ORIGINS') !== undefined) {
    problems.push('BETTER_AUTH_TRUSTED_ORIGINS must be unset: GOAL_PUBLIC_URL is the only trusted origin');
  }

  const secret = get('BETTER_AUTH_SECRET');
  const secrets = parseSecrets(get('BETTER_AUTH_SECRETS'), problems);
  if (secret === undefined && secrets === undefined) {
    problems.push('BETTER_AUTH_SECRET or BETTER_AUTH_SECRETS is required (openssl rand -hex 32)');
  }
  if (secret !== undefined && byteLength(secret) < minSecretBytes) {
    problems.push(`BETTER_AUTH_SECRET must be at least ${minSecretBytes} bytes`);
  }

  const proxySecret = get('GOAL_AUTH_PROXY_SECRET');
  if (proxySecret === undefined) {
    problems.push(
      'GOAL_AUTH_PROXY_SECRET is required: the secret goald sends with every request it forwards ' +
        '(goal up generates one for both; when you start them yourself, give both the same: openssl rand -hex 32)',
    );
  } else if (byteLength(proxySecret) < minSecretBytes) {
    problems.push(`GOAL_AUTH_PROXY_SECRET must be at least ${minSecretBytes} bytes`);
  } else if (proxySecret === secret || secrets?.some((s) => s.value === proxySecret)) {
    problems.push('GOAL_AUTH_PROXY_SECRET must not be a Better Auth secret');
  }

  const google = parseGoogle(get, problems);

  const bootstrapOwnerEmail = get('GOAL_BOOTSTRAP_OWNER_EMAIL')?.toLowerCase();
  if (bootstrapOwnerEmail !== undefined && !emailPattern.test(bootstrapOwnerEmail)) {
    problems.push('GOAL_BOOTSTRAP_OWNER_EMAIL must be an email address');
  }
  const smtp = parseSMTP(get, problems);

  const password = get('GOAL_PG_AUTH_PASSWORD') ?? devAuthPassword;
  const databaseURL =
    get('GOAL_AUTH_DATABASE_URL') ??
    `postgres://goal_auth:${encodeURIComponent(password)}@127.0.0.1:5432/goal?sslmode=disable`;

  if (problems.length > 0 || listen === undefined || publicURL === undefined || proxySecret === undefined) {
    throw new SettingsError(problems);
  }

  return { listen, publicURL, databaseURL, secret, secrets, google, bootstrapOwnerEmail, smtp, proxySecret };
}

function parseListen(value: string, problems: string[]): Settings['listen'] | undefined {
  const m = /^(\[[^\]]+\]|[^:]+):(\d{1,5})$/.exec(value);
  const port = Number(m?.[2]);
  if (!m?.[1] || !Number.isInteger(port) || port < 1 || port > 65535) {
    problems.push(`GOAL_AUTH_LISTEN: expected host:port, got ${JSON.stringify(value)}`);
    return undefined;
  }
  const host = m[1].replace(/^\[(.*)\]$/, '$1');
  // Every interface (only behind an https public URL, loadSettings) is for a container of the
  // service's own; there the proxy secret is what keeps all but goald out.
  if (!isLoopback(host) && !everyInterface(host)) {
    problems.push(
      'GOAL_AUTH_LISTEN must be loopback (127.0.0.1, [::1] or localhost), or every interface (0.0.0.0, [::]) in a ' +
        'container that only goald reaches: goald proxies to it',
    );
    return undefined;
  }
  return { host, port };
}

function parseOrigin(name: string, value: string, problems: string[]): string | undefined {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    problems.push(`${name}: not a URL`);
    return undefined;
  }
  if (url.protocol !== 'https:' && url.protocol !== 'http:') {
    problems.push(`${name} must be an http or https URL`);
    return undefined;
  }
  if (url.username || url.password || url.search || url.hash || (url.pathname !== '/' && url.pathname !== '')) {
    problems.push(`${name} must be an origin: scheme, host and port only`);
    return undefined;
  }
  if (
    url.protocol === 'http:' &&
    !isLoopback(url.hostname.replace(/^\[(.*)\]$/, '$1')) &&
    !localhostName(url.hostname)
  ) {
    problems.push(`${name} must be https unless its host is loopback`);
    return undefined;
  }
  return url.origin;
}

function parseSecrets(value: string | undefined, problems: string[]): VersionedSecret[] | undefined {
  if (value === undefined) return undefined;
  const secrets: VersionedSecret[] = [];
  for (const entry of value.split(',')) {
    const m = /^\s*(\d+):(.+?)\s*$/.exec(entry);
    if (!m?.[1] || !m[2]) {
      problems.push('BETTER_AUTH_SECRETS: expected "<version>:<secret>,…", newest first');
      return undefined;
    }
    const version = Number(m[1]);
    if (secrets.some((s) => s.version === version)) {
      problems.push(`BETTER_AUTH_SECRETS: version ${version} is listed twice`);
    } else if (byteLength(m[2]) < minSecretBytes) {
      problems.push(`BETTER_AUTH_SECRETS: version ${version} must be at least ${minSecretBytes} bytes`);
    }
    secrets.push({ version, value: m[2] });
  }
  return secrets;
}

function parseGoogle(get: (name: string) => string | undefined, problems: string[]): Settings['google'] {
  const mode = get('GOAL_AUTH_GOOGLE') ?? 'off';
  if (mode === 'off') return undefined;
  if (mode !== 'on') {
    problems.push('GOAL_AUTH_GOOGLE must be on or off');
    return undefined;
  }
  const clientId = get('GOOGLE_CLIENT_ID');
  const clientSecret = get('GOOGLE_CLIENT_SECRET');
  if (clientId === undefined || clientSecret === undefined) {
    problems.push('GOAL_AUTH_GOOGLE=on needs GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET');
    return undefined;
  }
  return { clientId, clientSecret };
}

function parseSMTP(get: (name: string) => string | undefined, problems: string[]): Settings['smtp'] {
  const url = get('GOAL_SMTP_URL') ?? defaultSMTPURL;
  let parsed: URL | undefined;
  try {
    parsed = new URL(url);
  } catch {
    // The value is never repeated: it may hold the server's password.
  }
  if (parsed?.protocol !== 'smtp:' && parsed?.protocol !== 'smtps:') {
    problems.push('GOAL_SMTP_URL must be an smtp:// or smtps:// URL');
  }

  const from = get('GOAL_SMTP_FROM') ?? defaultSMTPFrom;
  const address = /<([^<>]+)>\s*$/.exec(from)?.[1] ?? from;
  if (!emailPattern.test(address) || /[\r\n]/.test(from)) {
    problems.push('GOAL_SMTP_FROM must be an address or "Name <address>"');
  }

  const host = parsed?.hostname.replace(/^\[(.*)\]$/, '$1') ?? '';
  const smtp = parsed?.protocol === 'smtp:';
  const mode = get('GOAL_SMTP_PLAINTEXT') ?? 'off';
  if (mode !== 'on' && mode !== 'off') problems.push('GOAL_SMTP_PLAINTEXT must be on or off');
  const plaintextOn = mode === 'on';
  // Compose's Mailpit by its service name, which compose's DNS answers: any other name without
  // dots may be a host of the LAN through a DNS search domain, and sign-in codes would cross it.
  if (plaintextOn && !(smtp && (isLoopback(host) || host === composeMailpit))) {
    problems.push(
      `GOAL_SMTP_PLAINTEXT=on needs an smtp:// GOAL_SMTP_URL on loopback or compose's Mailpit (${composeMailpit}); ` +
        'any other server must offer STARTTLS',
    );
  }
  return { url, from, plaintext: smtp && (isLoopback(host) || plaintextOn) };
}

/**
 * localhostName is true for a name under .localhost (RFC 6761 §6.3), such as goal.localhost: browsers
 * resolve it to this machine themselves, so a public URL on it may be http, and a name of its own
 * keeps the session cookie from other local web servers, as cookies are not port-scoped (review L5).
 */
export function localhostName(host: string): boolean {
  return /^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+localhost$/.test(host.toLowerCase());
}

/** everyInterface is true for the unspecified addresses, 0.0.0.0 and ::. */
function everyInterface(host: string): boolean {
  return host === '0.0.0.0' || host === '::';
}

/** isLoopback is true for localhost, 127.0.0.0/8 and ::1. */
export function isLoopback(host: string): boolean {
  return host === 'localhost' || host === '::1' || /^127(\.\d{1,3}){3}$/.test(host);
}

function byteLength(s: string): number {
  return new TextEncoder().encode(s).length;
}

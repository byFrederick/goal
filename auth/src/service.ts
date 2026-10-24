import { type ServerType, serve } from '@hono/node-server';
import { createApp } from './app.js';
import { createAuth } from './auth.js';
import { checkRole, openPool } from './db.js';
import type { Logger } from './log.js';
import type { Mailer } from './mail/mailer.js';
import { SMTPMailer } from './mail/smtp.js';
import { type Env, loadSettings } from './settings.js';

export interface Service {
  /** The address it serves, http://<GOAL_AUTH_LISTEN>. */
  url: string;
  /** close stops serving and closes the database pool. */
  close(): Promise<void>;
}

/**
 * startService starts the auth service from env, refusing (with SettingsError, RoleError or
 * Better Auth's schema mismatch) before it serves anything: its settings must hold, it must
 * connect as a role that can do no more than Better Auth's DML, and the database must hold the
 * schema this Better Auth expects (goose migrations 00040 on). It then serves on loopback. Mail goes
 * through mailer, or else the SMTP server of GOAL_SMTP_URL.
 */
export async function startService(env: Env, log: Logger, mailer?: Mailer): Promise<Service> {
  const settings = loadSettings(env);
  const smtp = mailer ? undefined : new SMTPMailer(settings.smtp.url, settings.smtp.from);
  const mail: Mailer = mailer ?? (smtp as SMTPMailer);
  const pool = openPool(settings.databaseURL);
  pool.on('error', (e) => log.log('error', 'database.error', { error: e }));

  let server: ServerType;
  try {
    await checkRole(pool);
    const auth = createAuth({ settings, pool, log, mailer: mail });
    await (await auth.$context).explicitSchemaCheck?.();

    server = await new Promise<ServerType>((resolve, reject) => {
      const s = serve(
        {
          fetch: createApp(auth, settings.proxySecret).fetch,
          hostname: settings.listen.host,
          port: settings.listen.port,
        },
        () => resolve(s),
      );
      s.once('error', reject);
    });
  } catch (e) {
    smtp?.close();
    await pool.end();
    throw e;
  }

  const host = settings.listen.host.includes(':') ? `[${settings.listen.host}]` : settings.listen.host;
  log.log('info', 'auth.listening', { listen: `${host}:${settings.listen.port}`, public_url: settings.publicURL });

  return {
    url: `http://${host}:${settings.listen.port}`,
    close: () =>
      new Promise<void>((resolve, reject) => server.close((e) => (e ? reject(e) : resolve()))).finally(() => {
        smtp?.close();
        return pool.end();
      }),
  };
}

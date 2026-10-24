// The auth service's entry point: `node auth/dist/server.js` (task auth:build). It starts the
// service from its environment and stops it on SIGTERM or SIGINT; a refusal exits 1.
import type { SendSignInCode } from './auth.js';
import { jsonLogger } from './log.js';
import { startService } from './service.js';
import { SettingsError } from './settings.js';

const log = jsonLogger();

// This build has no mailer: a code is never sent, and only the fact is logged.
const sendSignInCode: SendSignInCode = async () => log.log('warn', 'otp.not_sent', { reason: 'no_mailer' });

try {
  const service = await startService(process.env, log, sendSignInCode);
  const stop = (signal: string) => {
    log.log('info', 'auth.stopping', { signal });
    service.close().finally(() => process.exit(0));
  };
  process.once('SIGTERM', () => stop('SIGTERM'));
  process.once('SIGINT', () => stop('SIGINT'));
} catch (e) {
  log.log('error', 'auth.refused', e instanceof SettingsError ? { problems: e.problems } : { error: e });
  process.exit(1);
}

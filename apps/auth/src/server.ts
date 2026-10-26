// The auth service's entry point: `node apps/auth/dist/server.js` (task auth:build). It starts the
// service from its environment and stops it on SIGTERM or SIGINT; a refusal exits 1.
import { jsonLogger } from './log.js';
import { startService } from './service.js';
import { SettingsError } from './settings.js';

const log = jsonLogger();

try {
  const service = await startService(process.env, log);
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

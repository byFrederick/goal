// The configuration the pinned Better Auth CLI reads (`auth generate --config src/generate.ts`):
// Goal's Better Auth setup on the database at GOAL_AUTH_DATABASE_URL, which must be set, with a
// throwaway secret, since comparing the schema reads only the tables. task auth:schema-check runs
// it against a scratch database at the goose head and fails on any difference; 00041 was made
// with it. `auth migrate` is never run: the schema changes only through goose migrations.
import { randomBytes } from 'node:crypto';
import { createAuth } from './auth.js';
import { openPool } from './db.js';
import { jsonLogger } from './log.js';
import { loadSettings } from './settings.js';

const url = process.env.GOAL_AUTH_DATABASE_URL;
if (!url) throw new Error('GOAL_AUTH_DATABASE_URL must name the scratch database to compare with');

export const auth = createAuth({
  settings: loadSettings({ GOAL_AUTH_DATABASE_URL: url, BETTER_AUTH_SECRET: randomBytes(32).toString('hex') }),
  pool: openPool(url),
  log: jsonLogger((line) => process.stderr.write(line)),
  sendSignInCode: async () => {},
});

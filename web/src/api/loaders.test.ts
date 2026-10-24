import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

// Route loaders load through ensureLoaded (queries.ts): a plain ensureQueryData or fetchQuery
// rejects when the fetch it shares is cancelled by the page being left (review H2).
const routes = join(import.meta.dirname, '..', 'routes');

describe('route loaders', () => {
  it('load through ensureLoaded only', () => {
    const raw = readdirSync(routes)
      .filter((f) => f.endsWith('.tsx'))
      .filter((f) => /\.(ensureQueryData|fetchQuery)\(/.test(readFileSync(join(routes, f), 'utf8')));
    expect(raw).toEqual([]);
  });
});

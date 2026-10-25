import { describe, expect, it } from 'vitest';
import config from '../wrangler.jsonc?raw';

describe('the deployed wrangler config', () => {
  // Terraform owns the one route; a workers.dev or preview URL would be a second public origin
  // that wakes goald's container on every hit (phase 4 review, L3).
  it('serves the Worker on its Terraform route only', () => {
    expect(config).toMatch(/"workers_dev":\s*false/);
    expect(config).toMatch(/"preview_urls":\s*false/);
    expect(config).not.toMatch(/"routes?":/);
  });
});

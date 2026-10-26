import { describe, expect, it } from 'vitest';
import config from '../../../deploy/cloudflare/wrangler.jsonc?raw';

describe('the deployed wrangler config', () => {
  // Terraform owns the one route; a workers.dev or preview URL would be a second public origin
  // that wakes goald's container on every hit (phase 4 review, L3).
  it('serves the Worker on its Terraform route only', () => {
    expect(config).toMatch(/"workers_dev":\s*false/);
    expect(config).toMatch(/"preview_urls":\s*false/);
    expect(config).not.toMatch(/"routes?":/);
  });

  // The install has no mail server for now: Google is the only way in, and the auth service binds
  // no SMTP URL, which Terraform makes no store secret for (cloudflare-operations.md).
  it('signs in with Google only, with no mail server', () => {
    expect(config).toMatch(/"GOAL_AUTH_GOOGLE":\s*"on"/);
    expect(config).toMatch(/"GOAL_AUTH_EMAIL":\s*"off"/);
    expect(config).not.toMatch(/GOAL_SMTP_/);
  });
});

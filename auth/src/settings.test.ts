import { describe, expect, it } from 'vitest';
import { loadSettings, SettingsError } from './settings.js';

// Throwaway values: 32 bytes and more, never real secrets.
const secret = 'a'.repeat(32);
const proxySecret = 'p'.repeat(32);
const base = { BETTER_AUTH_SECRET: secret, GOAL_AUTH_PROXY_SECRET: proxySecret };

function problems(env: Record<string, string>): string[] {
  try {
    loadSettings(env);
  } catch (e) {
    if (e instanceof SettingsError) return e.problems;
    throw e;
  }
  return [];
}

describe('loadSettings', () => {
  it('defaults to loopback, goald’s origin and compose’s database as goal_auth', () => {
    const s = loadSettings(base);

    expect(s.listen).toEqual({ host: '127.0.0.1', port: 8081 });
    expect(s.publicURL).toBe('http://127.0.0.1:8080');
    expect(s.databaseURL).toBe('postgres://goal_auth:goal-auth-dev-only@127.0.0.1:5432/goal?sslmode=disable');
    expect(s.google).toBeUndefined();
    expect(s.secret).toBe(secret);
    expect(s.bootstrapOwnerEmail).toBeUndefined();
    expect(s.smtp).toEqual({ url: 'smtp://127.0.0.1:1025', from: 'Goal <goal@localhost>' });
    expect(s.proxySecret).toBe(proxySecret);
  });

  it('reads the bootstrap owner trimmed and in lower case', () => {
    expect(loadSettings({ ...base, GOAL_BOOTSTRAP_OWNER_EMAIL: ' Ada@Example.com ' }).bootstrapOwnerEmail).toBe(
      'ada@example.com',
    );
  });

  it('takes an SMTP server with credentials and a named sender', () => {
    const s = loadSettings({
      ...base,
      GOAL_SMTP_URL: 'smtps://user:test-only@email-smtp.example.com:465',
      GOAL_SMTP_FROM: 'Goal <goal@example.com>',
    });

    expect(s.smtp).toEqual({
      url: 'smtps://user:test-only@email-smtp.example.com:465',
      from: 'Goal <goal@example.com>',
    });
  });

  it('builds the default database URL from GOAL_PG_AUTH_PASSWORD, escaped', () => {
    expect(loadSettings({ ...base, GOAL_PG_AUTH_PASSWORD: 'p@ss/word' }).databaseURL).toBe(
      'postgres://goal_auth:p%40ss%2Fword@127.0.0.1:5432/goal?sslmode=disable',
    );
  });

  it('reads versioned secrets, newest first', () => {
    const s = loadSettings({
      BETTER_AUTH_SECRETS: `2:${'b'.repeat(40)}, 1:${secret}`,
      GOAL_AUTH_PROXY_SECRET: proxySecret,
    });

    expect(s.secrets).toEqual([
      { version: 2, value: 'b'.repeat(40) },
      { version: 1, value: secret },
    ]);
  });

  it('takes an https public URL and normalizes it to its origin', () => {
    expect(loadSettings({ ...base, GOAL_PUBLIC_URL: 'https://goal.example.com/' }).publicURL).toBe(
      'https://goal.example.com',
    );
  });

  it('turns Google on with its client id and secret', () => {
    const s = loadSettings({
      ...base,
      GOAL_AUTH_GOOGLE: 'on',
      GOOGLE_CLIENT_ID: 'id.apps.example.com',
      GOOGLE_CLIENT_SECRET: 'test-only',
    });

    expect(s.google).toEqual({ clientId: 'id.apps.example.com', clientSecret: 'test-only' });
  });

  it.each([
    ['no secret', {}, 'BETTER_AUTH_SECRET or BETTER_AUTH_SECRETS is required'],
    ['a short secret', { BETTER_AUTH_SECRET: 'short' }, 'BETTER_AUTH_SECRET must be at least 32 bytes'],
    ['a short versioned secret', { BETTER_AUTH_SECRETS: `2:short,1:${secret}` }, 'version 2 must be at least 32 bytes'],
    ['a malformed versioned secret', { BETTER_AUTH_SECRETS: secret }, 'BETTER_AUTH_SECRETS: expected'],
    ['a repeated version', { BETTER_AUTH_SECRETS: `1:${secret},1:${secret}` }, 'version 1 is listed twice'],
    ['a public listen address', { ...base, GOAL_AUTH_LISTEN: '0.0.0.0:8081' }, 'GOAL_AUTH_LISTEN must be loopback'],
    ['a malformed listen address', { ...base, GOAL_AUTH_LISTEN: '127.0.0.1' }, 'GOAL_AUTH_LISTEN: expected host:port'],
    ['http off loopback', { ...base, GOAL_PUBLIC_URL: 'http://goal.example.com' }, 'must be https unless'],
    ['a public URL with a path', { ...base, GOAL_PUBLIC_URL: 'https://example.com/goal' }, 'an origin'],
    ['not a URL', { ...base, GOAL_PUBLIC_URL: 'goal.example.com' }, 'GOAL_PUBLIC_URL: not a URL'],
    [
      'BETTER_AUTH_URL other than GOAL_PUBLIC_URL',
      { ...base, GOAL_PUBLIC_URL: 'https://goal.example.com', BETTER_AUTH_URL: 'https://evil.example.com' },
      'BETTER_AUTH_URL must equal GOAL_PUBLIC_URL',
    ],
    ['extra trusted origins', { ...base, BETTER_AUTH_TRUSTED_ORIGINS: '*' }, 'BETTER_AUTH_TRUSTED_ORIGINS'],
    ['Google without its client', { ...base, GOAL_AUTH_GOOGLE: 'on' }, 'GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET'],
    ['an unknown Google mode', { ...base, GOAL_AUTH_GOOGLE: 'yes' }, 'GOAL_AUTH_GOOGLE must be on or off'],
    ['a bootstrap owner that is no address', { ...base, GOAL_BOOTSTRAP_OWNER_EMAIL: 'ada' }, 'an email address'],
    ['a mail server that is not SMTP', { ...base, GOAL_SMTP_URL: 'http://127.0.0.1:1025' }, 'GOAL_SMTP_URL must be'],
    ['a sender that is no address', { ...base, GOAL_SMTP_FROM: 'Goal' }, 'GOAL_SMTP_FROM must be'],
    ['no proxy secret', { BETTER_AUTH_SECRET: secret }, 'GOAL_AUTH_PROXY_SECRET is required'],
    [
      'a short proxy secret',
      { ...base, GOAL_AUTH_PROXY_SECRET: 'short' },
      'GOAL_AUTH_PROXY_SECRET must be at least 32 bytes',
    ],
    [
      'the Better Auth secret as the proxy secret',
      { ...base, GOAL_AUTH_PROXY_SECRET: secret },
      'must not be a Better Auth secret',
    ],
  ])('refuses %s', (_, env, want) => {
    expect(problems(env).join('\n')).toContain(want);
  });

  it('accepts BETTER_AUTH_URL equal to GOAL_PUBLIC_URL and other loopback addresses', () => {
    const s = loadSettings({
      ...base,
      GOAL_PUBLIC_URL: 'http://localhost:8080',
      BETTER_AUTH_URL: 'http://localhost:8080/',
      GOAL_AUTH_LISTEN: '[::1]:9081',
    });

    expect(s.publicURL).toBe('http://localhost:8080');
    expect(s.listen).toEqual({ host: '::1', port: 9081 });
  });

  it('lists every problem at once and never a secret value', () => {
    const leaked = 'leaked-secret-value';
    const got = problems({
      BETTER_AUTH_SECRET: leaked,
      GOAL_AUTH_PROXY_SECRET: proxySecret,
      GOAL_AUTH_LISTEN: '0.0.0.0:1',
      GOAL_AUTH_GOOGLE: 'on',
      GOOGLE_CLIENT_SECRET: leaked,
    });

    expect(got).toHaveLength(3);
    expect(got.join('\n')).not.toContain(leaked);
  });

  it('never repeats the SMTP URL, which holds the mail server’s password', () => {
    const got = problems({ ...base, GOAL_SMTP_URL: 'ftp://user:leaked-password@mail.example.com' });

    expect(got).toHaveLength(1);
    expect(got.join('\n')).not.toContain('leaked-password');
  });
});

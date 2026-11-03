import { describe, expect, it } from 'vitest';
import type { TemplateAdds } from '../../../api/types';
import {
  addsPreview,
  addToText,
  applyPatch,
  envName,
  fillAdds,
  modeOf,
  readEnv,
  removeEntry,
  setEntry,
  setImage,
  setList,
  setSize,
  setToolMode,
} from './env-file';

const file = `apiVersion: goal/v1
kind: Environment
spec:
  # what the box installs
  tools: [go@1.26]
  env:
    GH_TOKEN: ghp_0123456789abcdef # plain by mistake
    SENTRY_ORG: byfrederick
  mcp:
    linear:
      url: https://mcp.linear.app/mcp
      headers: { Authorization: "Bearer {{linear-key}}" }
      tools: { "delete_*": off }
`;

describe('environment files', () => {
  it('reads the spec leniently', () => {
    const spec = readEnv(file);
    expect(spec.tools).toEqual(['go@1.26']);
    expect(spec.env.SENTRY_ORG).toBe('byfrederick');
    expect(spec.mcp.linear?.tools).toEqual({ 'delete_*': 'off' });
    expect(readEnv('not: [valid').mcp).toEqual({});
  });

  it('names a file by its path', () => {
    expect(envName('environments/deploy.yaml')).toBe('deploy');
    expect(envName('factory.yaml')).toBeUndefined();
  });

  it('keeps comments and key order when it edits', () => {
    const out = setEntry(setSize(file, 'large'), 'env', 'SENTRY_ORG', 'acme');
    expect(out).toContain('# what the box installs');
    expect(out).toContain('# plain by mistake');
    expect(readEnv(out).size).toBe('large');
    expect(readEnv(out).env.SENTRY_ORG).toBe('acme');
  });

  it('removes a key and the parents it leaves empty', () => {
    const out = removeEntry(removeEntry(file, 'env', 'GH_TOKEN'), 'env', 'SENTRY_ORG');
    expect(out).not.toContain('env:');
    expect(setList(file, 'tools', [])).not.toContain('tools: [go');
    expect(setImage(file, ' ')).toBe(file);
  });

  it('renames a variable where it stands', () => {
    const out = setEntry(file, 'env', 'GITHUB_TOKEN', '{{gh-token}}', 'GH_TOKEN');
    expect(Object.keys(readEnv(out).env)).toEqual(['GITHUB_TOKEN', 'SENTRY_ORG']);
  });

  it('sets and clears a tool mode', () => {
    const out = setToolMode(file, 'linear', 'create_comment', 'ask');
    expect(readEnv(out).mcp.linear?.tools.create_comment).toBe('ask');
    expect(readEnv(setToolMode(out, 'linear', 'create_comment', undefined)).mcp.linear?.tools).toEqual({
      'delete_*': 'off',
    });
  });

  it('gives a tool the most specific mode', () => {
    const server = { headers: {}, env: {}, tools: { '*': 'ask', 'get_*': 'allow', delete_issue: 'off' } } as const;
    const s = { ...server, tools: { ...server.tools } };
    expect(modeOf(s, 'delete_issue')).toBe('off');
    expect(modeOf(s, 'get_issue')).toBe('allow');
    expect(modeOf(s, 'update_issue')).toBe('ask');
    expect(modeOf({ ...s, tools: {} }, 'x')).toBe('allow');
  });

  it('applies the plain-key fix of the authoring tests to the file spec', () => {
    // internal/authoring/secrets_test.go: replace /default/env/GH_TOKEN with "{{gh-token}}"
    const out = applyPatch(file, [{ op: 'replace', path: '/default/env/GH_TOKEN', value: '{{gh-token}}' }]);
    expect(readEnv(out).env.GH_TOKEN).toBe('{{gh-token}}');
    expect(out).toContain('# plain by mistake');
    expect(applyPatch(file, [{ op: 'remove', path: '/default/mcp/linear' }])).not.toContain('linear');
  });
});

const sentry: TemplateAdds = {
  tools: ['go@1.26', 'gh@2'],
  env: { SENTRY_ORG: 'byfrederick' },
  files: {},
  mcp: {
    sentry: {
      url: 'https://mcp.sentry.dev/mcp',
      headers: { Authorization: 'Bearer {{token}}' },
      tools: { 'update_*': 'ask', 'delete_*': 'off' },
    },
  },
};

describe('a template in a file', () => {
  it('fills the slots with the picked secret', () => {
    const out = fillAdds(sentry, { token: 'sentry-token' });
    expect(out.mcp.sentry?.headers?.Authorization).toBe('Bearer {{sentry-token}}');
  });

  it('adds only what the file lacks and keeps the rest', () => {
    const out = addToText(file, fillAdds(sentry, { token: 'sentry-token' }));
    const spec = readEnv(out);
    expect(spec.tools).toEqual(['go@1.26', 'gh@2']);
    expect(Object.keys(spec.mcp)).toEqual(['linear', 'sentry']);
    expect(spec.mcp.sentry?.tools).toEqual({ 'update_*': 'ask', 'delete_*': 'off' });
    expect(out).toContain('# what the box installs');
    expect(addToText('', fillAdds(sentry, { token: 'a-b' }))).toContain('kind: Environment');
  });

  it('previews the lines it adds', () => {
    const lines = addsPreview(fillAdds(sentry, { token: 'sentry-token' }));
    expect(lines).toContain('      Authorization: Bearer {{sentry-token}}');
    expect(lines.join('\n')).toContain('tools: { update_*: ask, delete_*: off }');
  });
});

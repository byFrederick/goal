import { describe, expect, it } from 'vitest';
import type { ToolTemplate } from '../../../api/types';
import { addToText, fillAdds, readEnv } from '../environments/env-file';
import {
  blankCustom,
  customAdds,
  customProblem,
  freeName,
  groupTemplates,
  inEnvironment,
  kindOf,
  pickedNames,
  suggested,
} from './apply-template';

const t = (name: string, adds: Partial<ToolTemplate['adds']>, slots: string[] = []): ToolTemplate => ({
  name,
  title: name[0]?.toUpperCase() + name.slice(1),
  description: `${name} things`,
  secrets: slots.map((slot) => ({ slot, title: slot, description: '', suggest: `${name}-${slot}` })),
  adds: { tools: [], env: {}, files: {}, mcp: {}, ...adds },
  test: null,
});

const sentry = t(
  'sentry',
  { mcp: { sentry: { url: 'https://mcp.sentry.dev/mcp', headers: { Authorization: 'Bearer {{token}}' }, tools: {} } } },
  ['token'],
);
const kubectl = t('kubectl', { tools: ['kubectl@1.31'], files: { '~/.kube/config': '{{kubeconfig}}' } }, [
  'kubeconfig',
]);
const datadog = t('datadog', { env: { DD_API_KEY: '{{key}}' } }, ['key']);

describe('the catalog', () => {
  it('groups by what a template adds, in the page’s order', () => {
    expect([kindOf(sentry), kindOf(kubectl), kindOf(datadog)]).toEqual(['mcp', 'cli', 'api']);
    const groups = groupTemplates([datadog, kubectl, sentry]);
    expect(groups.map((g) => g.kind)).toEqual(['mcp', 'cli', 'api']);
    expect(groupTemplates([datadog, kubectl, sentry], 'kube').map((g) => g.templates[0]?.name)).toEqual(['kubectl']);
  });

  it('knows what an environment already has', () => {
    const spec = readEnv('spec:\n  tools: [kubectl@1.31]\n  mcp: { sentry: { url: "https://x.example.com" } }\n');
    expect(inEnvironment(sentry, spec)).toBe(true);
    expect(inEnvironment(kubectl, spec)).toBe(true);
    expect(inEnvironment(datadog, spec)).toBe(false);
  });
});

describe('the secret of each slot', () => {
  it('is complete only when every slot has a name and a new one its value', () => {
    expect(pickedNames(sentry, {})).toBeUndefined();
    expect(pickedNames(sentry, suggested(sentry))).toBeUndefined();
    expect(pickedNames(sentry, { token: { kind: 'new', name: 'sentry-token', value: 'x' } })).toEqual({
      token: 'sentry-token',
    });
    expect(pickedNames(sentry, { token: { kind: 'existing', name: 'linear-key' } })).toEqual({ token: 'linear-key' });
  });
});

describe('writing a template into a file', () => {
  it('adds the lines with the picked secret and nothing else', () => {
    const out = addToText(
      'apiVersion: goal/v1\nkind: Environment\nspec: {}\n',
      fillAdds(sentry.adds, { token: 'sentry-token' }, { modes: { sentry: { 'delete_*': 'off' } } }),
    );
    expect(readEnv(out).mcp.sentry).toMatchObject({
      url: 'https://mcp.sentry.dev/mcp',
      headers: { Authorization: 'Bearer {{sentry-token}}' },
      tools: { 'delete_*': 'off' },
    });
  });
});

describe('a custom tool', () => {
  it('adds a server', () => {
    const tool = { ...blankCustom('mcp'), name: 'docs', url: 'https://docs.example.com/mcp', token: 'docs-key' };
    expect(customProblem(tool)).toBeUndefined();
    expect(customAdds(tool).mcp.docs?.headers).toEqual({ Authorization: 'Bearer {{docs-key}}' });
    expect(customProblem({ ...tool, url: 'ftp://x' })).toMatch(/address/);
  });

  it('adds a command-line tool with its variables and files', () => {
    const tool = {
      ...blankCustom('cli'),
      install: 'kubectl@1.31',
      variables: [{ key: 'KUBE_CONTEXT', value: 'staging', secret: '' }],
      files: [{ key: '~/.kube/config', value: '', secret: 'kube-staging' }],
    };
    expect(customProblem(tool)).toBeUndefined();
    expect(customAdds(tool)).toEqual({
      tools: ['kubectl@1.31'],
      env: { KUBE_CONTEXT: 'staging' },
      files: { '~/.kube/config': '{{kube-staging}}' },
      mcp: {},
    });
    expect(customProblem({ ...tool, install: 'kubectl' })).toMatch(/spec/);
  });

  it('adds an API as variables', () => {
    const tool = {
      ...blankCustom('api'),
      variables: [{ key: 'API_BASE', value: 'https://api.example.com', secret: '' }],
    };
    expect(customProblem(tool)).toBeUndefined();
    expect(customAdds(tool).env).toEqual({ API_BASE: 'https://api.example.com' });
    expect(customProblem({ ...tool, variables: [] })).toMatch(/variables/);
  });
});

describe('a new secret’s suggested name', () => {
  it('is the name as it is while the organization has no secret of it', () => {
    expect(freeName('sentry-token', new Set(['other']))).toBe('sentry-token');
  });

  it('becomes the first free name-2, name-3… when goald would refuse it', () => {
    expect(freeName('sentry-token', new Set(['sentry-token']))).toBe('sentry-token-2');
    expect(freeName('sentry-token', new Set(['sentry-token', 'sentry-token-2']))).toBe('sentry-token-3');
  });

  it('is applied to every slot of a template', () => {
    const sentry = t('sentry', {}, ['token']);
    expect(suggested(sentry, new Set(['sentry-token']))).toEqual({
      token: { kind: 'new', name: 'sentry-token-2', value: '' },
    });
  });
});

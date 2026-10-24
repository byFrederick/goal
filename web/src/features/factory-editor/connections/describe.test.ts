import { describe, expect, it } from 'vitest';
import type { DefinitionProblem } from '../../../api/types';
import type { Model, ModelConnection } from '../../../shared/definition/model';
import { agentsWithAccess, connectionKind, connectionStatus, served, signIn } from './describe';

const github: ModelConnection = {
  name: 'github',
  type: 'mcp',
  legacy: true,
  settings: {
    transport: 'http',
    url: 'https://api.example.com/mcp/',
    headers: { Authorization: 'env:GOAL_GITHUB_TOKEN' },
  },
};
const docs: ModelConnection = {
  name: 'aws-docs',
  type: 'mcp',
  legacy: true,
  settings: { transport: 'stdio', image: 'ghcr.io/acme/aws-docs:1.2.0', env: { REGION: 'env:AWS_REGION' } },
};
const local: ModelConnection = {
  name: 'tool',
  type: 'mcp',
  legacy: true,
  settings: { transport: 'stdio', command: ['run', '--token', 'env:TOOL_TOKEN'] },
};
const jira: ModelConnection = { name: 'jira', connector: 'jira@2', credential: 'acme-jira', agents: ['triager'] };

const problem = (code: string, pointer: string, severity: DefinitionProblem['severity'] = 'error') =>
  ({ code, pointer, severity, message: code }) as DefinitionProblem;

describe('connections', () => {
  it('names each kind of connection', () => {
    expect(connectionKind(github)).toBe('Remote MCP server');
    expect(connectionKind(docs)).toBe('MCP server image');
    expect(connectionKind(local)).toBe('Command');
    expect(connectionKind({ name: 'b', type: 'builtin', legacy: true })).toBe('Built-in');
    expect(connectionKind({ name: 'a', type: 'aws', legacy: true })).toBe('AWS role');
    expect(connectionKind(jira)).toBe('jira connector');
  });

  it('says how a connection signs in: env references, a credential, or nothing', () => {
    expect(signIn(github)).toBe('Uses env:GOAL_GITHUB_TOKEN');
    expect(signIn(docs)).toBe('Uses env:AWS_REGION');
    expect(signIn(local)).toBe('Uses env:TOOL_TOKEN');
    expect(signIn(jira)).toBe('Uses the credential acme-jira');
    expect(signIn({ name: 'b', type: 'builtin', legacy: true })).toBe('No sign-in');
  });

  it('knows what the gateway serves before phase 4', () => {
    expect(served(github)).toBe(true);
    expect(served(docs)).toBe(true);
    expect(served(local)).toBe(false);
    expect(served({ name: 'a', type: 'aws', legacy: true })).toBe(false);
  });

  it('takes the status from B’s problems at the connection, not from a test', () => {
    expect(connectionStatus(github, 0, [])).toEqual({ tone: 'ok', text: 'No problems' });
    expect(
      connectionStatus(github, 0, [problem('secret-env-unset', '/connections/0/settings/headers/Authorization')]),
    ).toEqual({ tone: 'rework', text: 'Variable not set' });
    expect(connectionStatus(jira, 1, [problem('credentials-unavailable', '/connections/1/credential')])).toEqual({
      tone: 'rework',
      text: 'Credentials arrive with phase 4',
    });
    expect(connectionStatus(github, 0, [problem('secret-env-unset', '/connections/10/x')])).toEqual({
      tone: 'ok',
      text: 'No problems',
    });
    expect(connectionStatus(local, 0, [])).toEqual({ tone: 'muted', text: 'Not served until a later phase' });
    expect(
      connectionStatus(github, 0, [problem('a', '/connections/0/url'), problem('b', '/connections/0/x', 'warning')]),
    ).toEqual({ tone: 'rework', text: '2 problems' });
  });

  it('lists the agents with access: a connector-form entry’s own list, else each agent’s connections', () => {
    const model = {
      agents: {
        agents: [
          { name: 'triager', connections: [] },
          { name: 'reviewer', connections: ['github'] },
        ],
      },
    } as unknown as Model;
    expect(agentsWithAccess(model, github)).toEqual(['reviewer']);
    expect(agentsWithAccess(model, jira)).toEqual(['triager']);
  });
});

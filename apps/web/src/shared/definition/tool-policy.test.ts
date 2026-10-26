import { describe, expect, it } from 'vitest';
import { legacyRules, toolPolicy } from './tool-policy';

describe('an agent’s tool policy', () => {
  it('allows the groups set to Allowed and denies the others, as goald compiles them', () => {
    const policy = toolPolicy({
      name: 'engineer',
      builtins: { read: 'allowed', edit: 'allowed', run: { mode: 'allowed', commands: ['go', 'git'] }, web: 'off' },
    });
    expect(policy.allowed).toEqual(['Read', 'Grep', 'Glob', 'Edit', 'Write', 'Bash(go:*)', 'Bash(git:*)']);
    expect(policy.denied).toEqual(['WebFetch', 'WebSearch', 'Task']);
    expect(policy.asked).toEqual([]);
  });

  it('runs any command when Run commands names none, and denies Bash when it is off', () => {
    expect(toolPolicy({ name: 'a', builtins: { run: { mode: 'allowed' } } }).allowed).toEqual(['Bash']);
    expect(toolPolicy({ name: 'a', builtins: { read: 'allowed' } }).denied).toContain('Bash');
  });

  it('serves Ask me as Off and says which groups asked', () => {
    const policy = toolPolicy({ name: 'a', builtins: { read: 'allowed', web: 'ask', run: { mode: 'ask' } } });
    expect(policy.denied).toEqual(['Edit', 'Write', 'WebFetch', 'WebSearch', 'Task', 'Bash']);
    expect(policy.asked).toEqual(['web', 'run']);
  });

  it('gives an agent without settings the worker’s read-only tools', () => {
    const policy = toolPolicy({ name: 'a' });
    expect(policy.allowed).toEqual(['Read', 'Grep', 'Glob']);
    expect(policy.denied).toEqual(['Edit', 'Write', 'WebFetch', 'WebSearch', 'Task', 'Bash']);
  });

  it('shows a legacy tools block’s rules as they are written', () => {
    const policy = toolPolicy(
      { name: 'a', custom_tools: true },
      { allow: ['Read', 'Bash'], deny: ['Bash(aws * delete*)'] },
    );
    expect(policy).toEqual({ allowed: ['Read', 'Bash'], denied: ['Bash(aws * delete*)'], asked: [] });
  });
});

describe('a legacy tools block', () => {
  const yaml = `apiVersion: goal/v1
kind: Factory
spec:
  agents:
    - name: triager
      prompt: ./agents/triager.md
    - name: investigator
      prompt: ./agents/investigator.md
      tools:
        allow: [Read, Grep, Bash]
        deny: ["Bash(aws * delete*)", "Bash(kubectl delete*)"]
`;

  it('is read from the agent’s entry in factory.yaml', async () => {
    expect(await legacyRules(yaml, 'investigator')).toEqual({
      allow: ['Read', 'Grep', 'Bash'],
      deny: ['Bash(aws * delete*)', 'Bash(kubectl delete*)'],
    });
  });

  it('is absent for an agent without one, an unknown agent or unreadable YAML', async () => {
    expect(await legacyRules(yaml, 'triager')).toBeUndefined();
    expect(await legacyRules(yaml, 'nobody')).toBeUndefined();
    expect(await legacyRules('spec: [', 'investigator')).toBeUndefined();
  });
});

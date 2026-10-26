import { describe, expect, it } from 'vitest';
import { parseRule, type RuleText, serializeRule } from './rule-md';

describe('a rule file', () => {
  it.each<[string, RuleText]>([
    ['a description only', { description: 'Keep secrets out of commits.', body: '# Secrets\n\nNever commit one.\n' }],
    [
      'appliesTo and paths',
      {
        description: 'Ship small changes.',
        appliesTo: { agents: ['engineer'], stations: ['build'], lanes: ['backend'], workTypes: ['feature'] },
        paths: ['src/**/*.ts', 'web/**'],
        body: 'One change per pull request.',
      },
    ],
    [
      'a description with a colon and quotes',
      { description: 'Tone: plain "sentence case", no \'jargon\'.', body: 'Write for operators.' },
    ],
    ['an empty body', { description: 'Empty on purpose.', body: '' }],
  ])('round-trips %s', (_name, rule) => {
    expect(parseRule(serializeRule(rule))).toEqual(rule);
  });

  it('leaves appliesTo and paths out when they are empty, so every agent gets it', () => {
    const text = serializeRule({ description: 'Everyone.', appliesTo: { agents: [] }, paths: [], body: 'x' });
    expect(text).toBe('---\ndescription: Everyone.\n---\n\nx');
  });

  it('writes the frontmatter the config reads', () => {
    const text = serializeRule({
      description: 'Plain.',
      appliesTo: { agents: ['engineer'] },
      paths: ['src/**'],
      body: 'Body.',
    });
    expect(text).toBe(
      '---\ndescription: Plain.\nappliesTo:\n  agents:\n    - engineer\npaths:\n  - src/**\n---\n\nBody.',
    );
  });

  it('reads a file without frontmatter as all body, with no description', () => {
    expect(parseRule('# Just text\n')).toEqual({ description: '', body: '# Just text\n' });
  });
});

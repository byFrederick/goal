import { describe, expect, it } from 'vitest';
import { skillDescription } from './skill-md';

describe('a skill’s description', () => {
  it('is the description of SKILL.md’s front matter', () => {
    const text = '---\nname: incident-triage\ndescription: Severity and sizing rules\n---\n\n# Triage\n';
    expect(skillDescription(text)).toBe('Severity and sizing rules');
  });

  it('unquotes a quoted value and joins a folded one', () => {
    expect(skillDescription('---\ndescription: "Rules: severity, sizing"\n---\n')).toBe('Rules: severity, sizing');
    expect(skillDescription("---\ndescription: 'It''s short'\n---\n")).toBe("It's short");
    expect(
      skillDescription('---\nname: x\ndescription: >\n  Read-only playbook\n  for ECS and RDS\nlicense: MIT\n---\n'),
    ).toBe('Read-only playbook for ECS and RDS');
  });

  it('falls back to the first paragraph, and is undefined for an empty file', () => {
    expect(skillDescription('# Postmortems\n\nWrite one when the item\nbecomes a postmortem.\n\nMore.')).toBe(
      'Write one when the item becomes a postmortem.',
    );
    expect(skillDescription('---\nname: x\n---\n')).toBeUndefined();
    expect(skillDescription('')).toBeUndefined();
  });
});

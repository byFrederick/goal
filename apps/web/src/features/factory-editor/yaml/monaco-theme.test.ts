import { describe, expect, it } from 'vitest';
import { goalTheme } from './monaco-theme';

const token = (name: string) => `#${name}`;

describe('goalTheme', () => {
  it('builds on vs-dark in the dark theme', () => {
    expect(goalTheme('dark', token).base).toBe('vs-dark');
  });
  it('builds on vs in the light theme', () => {
    expect(goalTheme('light', token).base).toBe('vs');
  });
  it('takes the background from the shell token', () => {
    expect(goalTheme('dark', token).colors['editor.background']).toBe('#shell');
  });
});

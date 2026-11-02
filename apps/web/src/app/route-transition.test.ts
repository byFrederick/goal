import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

// The route change must never show two pages at once (MOTION.md, Route change). jsdom has no View
// Transitions, so these tests pin the rules in styles.css that make the old picture leave at once.
const css = readFileSync(new URL('../styles.css', import.meta.url), 'utf8');

// The declarations of the first rule whose selector list contains `selector` (outside media queries).
function rule(selector: string, from = 0): string {
  const re = new RegExp(`(^|\\n)([^{}@]*${selector.replace(/[()]/g, '\\$&')}[^{}]*)\\{([^}]*)\\}`, 'g');
  re.lastIndex = from;
  const m = re.exec(css);
  if (!m) throw new Error(`no rule for ${selector}`);
  return m[3] ?? '';
}
const reduced = css.slice(css.indexOf('@media (prefers-reduced-motion: reduce) {', css.indexOf('view-fade-in')));

describe('route change view transition', () => {
  it('hides the old picture of the content and the shell', () => {
    for (const name of ['root', 'sidebar', 'topbar', 'main']) {
      expect(rule(`::view-transition-old(${name})`)).toContain('display: none');
    }
  });

  it('fades in and raises only the new content, with tokens', () => {
    const decl = rule('::view-transition-new(main)');
    expect(decl).toContain('animation: view-route-in var(--duration-quick) var(--ease-out) both');
    expect(decl).toContain('mix-blend-mode: normal');
    expect(css).not.toContain('view-route-out');
  });

  it('does not animate the groups, so the pages never stretch', () => {
    for (const name of ['root', 'sidebar', 'topbar', 'main']) {
      expect(rule(`::view-transition-group(${name})`)).toContain('animation: none');
    }
  });

  it('holds the new shell pictures still with normal blending', () => {
    for (const name of ['root', 'sidebar', 'topbar']) {
      const decl = rule(`::view-transition-new(${name})`);
      expect(decl).toContain('animation: none');
      expect(decl).toContain('mix-blend-mode: normal');
    }
  });

  it('under reduced motion fades the new content by opacity only, and the old one stays hidden', () => {
    expect(reduced).toContain('::view-transition-new(main)');
    expect(reduced).toContain('animation-name: view-fade-in');
    expect(reduced).not.toContain('::view-transition-old(main)');
    expect(css).toMatch(/@keyframes view-fade-in \{\s*from \{\s*opacity: 0;\s*\}\s*to \{\s*opacity: 1;\s*\}\s*\}/);
  });
});

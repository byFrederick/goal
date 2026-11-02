// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { settleTiming } from './station-dnd';

// The token values as styles.css sets them on the page.
function setTokens(duration: string, easing: string) {
  document.documentElement.style.setProperty('--duration-fast', duration);
  document.documentElement.style.setProperty('--ease-out', easing);
}

function setReducedMotion(reduce: boolean) {
  window.matchMedia = ((query: string) => ({
    matches: reduce && query.includes('prefers-reduced-motion'),
    media: query,
    addEventListener() {},
    removeEventListener() {},
  })) as unknown as typeof window.matchMedia;
}

beforeEach(() => {
  setReducedMotion(false);
  setTokens('250ms', 'cubic-bezier(0.22, 1, 0.36, 1)');
});

afterEach(() => {
  document.documentElement.removeAttribute('style');
});

describe('settleTiming', () => {
  it('settles a dropped station over the fast duration with the out easing', () => {
    expect(settleTiming()).toEqual({ duration: 250, easing: 'cubic-bezier(0.22, 1, 0.36, 1)' });
  });

  it('snaps into place under reduced motion, with no settle', () => {
    setReducedMotion(true);
    expect(settleTiming()).toBeNull();
  });

  it('snaps into place when the tokens are not on the page', () => {
    document.documentElement.removeAttribute('style');
    expect(settleTiming()).toBeNull();
  });
});

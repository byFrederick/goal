// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { handleSkippedTransitions } from './view-transition';

const unhandled = vi.fn();
process.on('unhandledRejection', unhandled);

function skipped() {
  const abort = () => Promise.reject(new DOMException('Transition was skipped', 'AbortError'));
  return { ready: abort(), finished: abort(), updateCallbackDone: abort() };
}

describe('handleSkippedTransitions', () => {
  afterEach(() => {
    unhandled.mockClear();
    // biome-ignore lint/suspicious/noExplicitAny: the test removes what it faked
    delete (document as any).startViewTransition;
  });

  it('leaves a skipped transition no unhandled rejection and returns the original object', async () => {
    const transition = skipped();
    const original = vi.fn(function (this: unknown) {
      expect(this).toBe(document);
      return transition;
    });
    // biome-ignore lint/suspicious/noExplicitAny: jsdom has no View Transitions
    (document as any).startViewTransition = original;
    handleSkippedTransitions();

    // biome-ignore lint/suspicious/noExplicitAny: as above
    const got = (document as any).startViewTransition(() => {});
    await new Promise((r) => setTimeout(r, 20));

    expect(got).toBe(transition);
    expect(unhandled).not.toHaveBeenCalled();
  });

  it('does nothing in a browser without View Transitions', () => {
    handleSkippedTransitions();
    expect('startViewTransition' in document).toBe(false);
  });
});

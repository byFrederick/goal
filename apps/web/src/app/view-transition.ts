// A browser skips a View Transition when another starts or the tab is hidden, and then rejects its `ready`,
// `finished` and `updateCallbackDone` promises with an AbortError. The router starts the route-change
// transitions itself, so this wraps `document.startViewTransition` once at boot and gives each promise a
// no-op handler (MOTION.md, Implementation). The original transition object is returned untouched.
type Start = (...args: unknown[]) => ViewTransitionLike;
interface ViewTransitionLike {
  ready?: Promise<unknown>;
  finished?: Promise<unknown>;
  updateCallbackDone?: Promise<unknown>;
}

export function handleSkippedTransitions(doc: Document = document): void {
  const target = doc as unknown as { startViewTransition?: Start };
  const original = target.startViewTransition;
  if (typeof original !== 'function') return;
  target.startViewTransition = (...args: unknown[]) => {
    const transition = original.apply(doc, args);
    transition?.ready?.catch(() => {});
    transition?.finished?.catch(() => {});
    transition?.updateCallbackDone?.catch(() => {});
    return transition;
  };
}

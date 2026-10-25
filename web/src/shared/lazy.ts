// Code loaded on first use. React.lazy keeps a failed import for good: one lost response for a
// chunk (a dropped connection) left the view on the error screen until the page reloaded, even
// after Try again. These ask for the chunk again first, after a pause (a connection that just
// dropped usually still fails within milliseconds).
import { type ComponentType, type LazyExoticComponent, lazy } from 'react';

/** How many times a chunk is asked for before its view shows the error. */
const attempts = 3;

/** The pause before each new try, in ms; the last one repeats. */
const pauses = [300, 1000];

/** Runs `load` until it resolves, at most `tries` times; the last failure is the one thrown. */
export async function retryImport<T>(load: () => Promise<T>, tries = attempts): Promise<T> {
  for (let i = 1; ; i++) {
    try {
      return await load();
    } catch (err) {
      if (i >= tries) throw err;
      await new Promise((resolve) => setTimeout(resolve, pauses[Math.min(i, pauses.length) - 1]));
    }
  }
}

/** React.lazy over `load`, asking for the chunk again when it fails to arrive. */
export function lazyRetrying<P>(load: () => Promise<ComponentType<P>>): LazyExoticComponent<ComponentType<P>> {
  return lazy(() => retryImport(load).then((component) => ({ default: component })));
}

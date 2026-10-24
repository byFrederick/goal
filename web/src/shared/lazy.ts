// Code loaded on first use. React.lazy keeps a failed import for good: one lost response for a
// chunk (a dropped connection) left the view on the error screen until the page reloaded, even
// after Try again. These ask for the chunk again first.
import { type ComponentType, type LazyExoticComponent, lazy } from 'react';

/** How many times a chunk is asked for before its view shows the error. */
const attempts = 3;

/** Runs `load` until it resolves, at most `tries` times; the last failure is the one thrown. */
export async function retryImport<T>(load: () => Promise<T>, tries = attempts): Promise<T> {
  for (let i = 1; ; i++) {
    try {
      return await load();
    } catch (err) {
      if (i >= tries) throw err;
    }
  }
}

/** React.lazy over `load`, asking for the chunk again when it fails to arrive. */
export function lazyRetrying<P>(load: () => Promise<ComponentType<P>>): LazyExoticComponent<ComponentType<P>> {
  return lazy(() => retryImport(load).then((component) => ({ default: component })));
}

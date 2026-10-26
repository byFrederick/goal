import type { Plugin } from 'vite';
import { createMockApi } from './api.ts';

/** Serves the mock API from the Vite dev and preview servers instead of proxying to goald. */
export function mockApi(options: { live?: boolean } = {}): Plugin {
  return {
    name: 'goal-mock-api',
    configureServer(server) {
      const api = createMockApi(options);
      server.middlewares.use(api.handle);
      server.httpServer?.on('close', api.close);
    },
    configurePreviewServer(server) {
      const api = createMockApi(options);
      server.middlewares.use(api.handle);
      server.httpServer?.on('close', api.close);
    },
  };
}

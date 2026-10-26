import { cloudflareTest } from '@cloudflare/vitest-plugin';
import { defineConfig } from 'vitest/config';

// The Worker in the Workers runtime (Miniflare), its containers replaced by stand-ins (test/).
// Vitest's module cache lives in the workspace root's node_modules, which the runtime cannot read.
export default defineConfig({
  plugins: [cloudflareTest({ wrangler: { configPath: './test/wrangler.jsonc' } })],
  test: { include: ['test/**/*.test.ts'], fsModuleCache: false },
});

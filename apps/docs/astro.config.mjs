// @ts-check
import starlight from '@astrojs/starlight';
import { defineConfig, passthroughImageService } from 'astro/config';
import starlightLlmsTxt from 'starlight-llms-txt';
import starlightOpenAPI, { openAPISidebarGroups } from 'starlight-openapi';

// The site is served at https://goal.byfrederick.work/docs. The image service is the
// passthrough one, so no image library (sharp) is installed.
export default defineConfig({
  site: 'https://goal.byfrederick.work',
  base: '/docs',
  // Files land under dist/docs/, so the build serves at /docs from any static host.
  outDir: './dist/docs',
  image: { service: passthroughImageService() },
  integrations: [
    starlight({
      title: 'Goal docs',
      description: 'Product documentation for Goal, the platform for AI agent factories.',
      customCss: ['./src/styles/goal.css'],
      expressiveCode: { styleOverrides: { borderRadius: '14px' } },
      plugins: [
        // The API reference: one page per operation, rendered from the contract at build time.
        starlightOpenAPI([
          {
            base: 'reference/api',
            schema: '../../contracts/api/openapi.yaml',
          },
        ]),
        // llms.txt files for agents: the whole site, a small version, a full version and one file per section.
        starlightLlmsTxt({
          projectName: 'Goal',
          description:
            'Goal is a config-driven platform for AI agent factories: each factory’s stations, agents and work types are data.',
          customSets: [
            { label: 'Getting started', paths: ['getting-started', 'getting-started/**'] },
            { label: 'Concepts', paths: ['concepts', 'concepts/**'] },
            { label: 'Guides', paths: ['guides', 'guides/**'] },
            { label: 'Reference', paths: ['reference', 'reference/**'] },
            { label: 'Changelog', paths: ['changelog', 'changelog/**'] },
          ],
        }),
      ],
      sidebar: [
        { label: 'Getting started', items: [{ autogenerate: { directory: 'getting-started' } }] },
        { label: 'Concepts', items: [{ autogenerate: { directory: 'concepts' } }] },
        { label: 'Guides', items: [{ autogenerate: { directory: 'guides' } }] },
        // The generated reference pages (tools/gendocs, task docs:ref:gen), then the API reference.
        {
          label: 'Reference',
          items: [
            { slug: 'reference' },
            { slug: 'reference/cli' },
            { slug: 'reference/definition' },
            { slug: 'reference/environment' },
            { slug: 'reference/glossary' },
            ...openAPISidebarGroups,
          ],
        },
        { label: 'Changelog', items: [{ autogenerate: { directory: 'changelog' } }] },
      ],
    }),
  ],
});

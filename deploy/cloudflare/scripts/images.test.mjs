// node --test scripts/: the deploy's image list and generated config, without Docker.
import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';
import { describe, it } from 'node:test';
import { fileURLToPath } from 'node:url';
import { deployConfig, images, inputs, parseJsonc } from './images.mjs';

const configDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const rootDir = path.resolve(configDir, '../..');
const config = parseJsonc(readFileSync(path.join(configDir, 'wrangler.jsonc'), 'utf8'));
const account = '0123456789abcdef0123456789abcdef';
const ref = (repository, n) => `registry.cloudflare.com/${account}/${repository}@sha256:${String(n).repeat(64)}`;
const refs = {
  'goal-goald': ref('goal-goald', 1),
  'goal-auth': ref('goal-auth', 2),
  'goal-runbox-goal-agent': ref('goal-runbox-goal-agent', 3),
};

describe('images', () => {
  it("lists wrangler.jsonc's three Dockerfiles under wrangler's repository names", () => {
    assert.deepEqual(
      images(config, configDir, rootDir).map((i) => [i.repository, i.dockerfile, i.context]),
      [
        ['goal-goald', '../../cmd/goald/Dockerfile', '../..'],
        ['goal-auth', '../../apps/auth/Dockerfile', '../..'],
        ['goal-runbox-goal-agent', '../../apps/worker/Dockerfile', '../..'],
      ],
    );
  });

  it('dates each image by paths that cover every file its Dockerfile copies from the context', () => {
    for (const [dockerfile, paths] of Object.entries(inputs)) {
      const covered = (p) => paths.some((input) => (input.endsWith('/') ? `${p}/`.startsWith(input) : p === input));
      assert.ok(covered(dockerfile), `${dockerfile} is not among its own inputs`);
      assert.ok(covered(`${dockerfile}.dockerignore`), `${dockerfile}.dockerignore is not among its inputs`);
      for (const line of readFileSync(path.join(rootDir, dockerfile), 'utf8').split('\n')) {
        const words = line.trim().split(/\s+/);
        if (words[0] !== 'COPY' || words.some((w) => w.startsWith('--from='))) continue;
        for (const source of words.slice(1, -1).filter((w) => !w.startsWith('--'))) {
          assert.ok(existsSync(path.join(rootDir, source)), `${dockerfile}: ${source} does not exist`);
          assert.ok(covered(source.replace(/\/$/, '')), `${dockerfile} copies ${source}, which its inputs miss`);
        }
      }
    }
  });
});

describe('deployConfig', () => {
  const out = deployConfig(config, refs);
  const byClass = Object.fromEntries(out.containers.map((c) => [c.class_name, c]));

  it('pins every Dockerfile image to its digest reference', () => {
    assert.equal(byClass.Goald.image, refs['goal-goald']);
    assert.equal(byClass.Auth.image, refs['goal-auth']);
    assert.deepEqual(byClass.RunBox.images, { 'goal-agent': { image: refs['goal-runbox-goal-agent'] } });
    assert.equal(byClass.Goald.image_build_context, undefined);
    assert.equal(byClass.Auth.image_build_context, undefined);
  });

  it('keeps every other key as it is', () => {
    const strip = (c) =>
      c.containers.map(({ image: _i, image_build_context: _b, images: named, ...rest }) => ({
        ...rest,
        images: named && Object.keys(named),
      }));
    assert.deepEqual({ ...out, containers: strip(out) }, { ...config, containers: strip(config) });
    assert.equal(config.containers[0].image, '../../cmd/goald/Dockerfile', 'config itself is left as it was');
  });

  it("keeps the docs site's static assets as wrangler.jsonc has them", () => {
    const assets = {
      binding: 'ASSETS',
      directory: '../../apps/docs/dist',
      html_handling: 'auto-trailing-slash',
      not_found_handling: '404-page',
      run_worker_first: ['/*', '!/docs', '!/docs/*'],
    };
    assert.deepEqual(config.assets, assets);
    assert.deepEqual(out.assets, assets);
  });

  it('refuses a missing reference, a tag, or a digest of another repository', () => {
    assert.throws(() => deployConfig(config, { ...refs, 'goal-auth': undefined }), /no image reference for goal-auth/);
    assert.throws(
      () => deployConfig(config, { ...refs, 'goal-auth': `registry.cloudflare.com/${account}/goal-auth:latest` }),
      /goal-auth: not a digest-pinned reference/,
    );
    assert.throws(() => deployConfig(config, { ...refs, 'goal-auth': refs['goal-goald'] }), /goal-auth: not a digest/);
  });
});

describe('parseJsonc', () => {
  it('drops comments and trailing commas but not their look-alikes inside strings', () => {
    assert.deepEqual(parseJsonc('{\n  // a\n  "u": "https://x/*y*/", /* b */\n  "l": [1, 2,],\n}'), {
      u: 'https://x/*y*/',
      l: [1, 2],
    });
  });
});

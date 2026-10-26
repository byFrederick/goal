// The production deploy's container images (scripts/deploy.sh, task deploy:worker): which image
// each Dockerfile of wrangler.jsonc builds, the paths whose last commit dates it
// (SOURCE_DATE_EPOCH), and the config wrangler deploys, with digest-pinned references in place of
// the Dockerfile paths. wrangler.jsonc keeps the Dockerfile paths for `wrangler dev`.
//
//   node scripts/images.mjs list                      one line per image: repository, Dockerfile,
//                                                     build context, inputs (tab-separated)
//   node scripts/images.mjs config <repo>=<ref>...    the deploy config, on stdout
//   node scripts/images.mjs field <path>              one field of the JSON on stdin
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

// The paths, relative to the repository root, whose content goes into each image: every COPY
// source of the Dockerfile, the Dockerfile and its Dockerfile.dockerignore (images.test.mjs checks it).
// An image's SOURCE_DATE_EPOCH and tag are the last commit that changed one of them.
export const inputs = {
  'cmd/goald/Dockerfile': [
    'cmd/goald/',
    'internal/',
    'db/migrations/',
    'contracts/definitions/',
    'apps/web/',
    'packages/api-client/',
    'go.mod',
    'go.sum',
    'package.json',
    'pnpm-lock.yaml',
    'pnpm-workspace.yaml',
  ],
  'apps/auth/Dockerfile': ['apps/auth/', 'package.json', 'pnpm-lock.yaml', 'pnpm-workspace.yaml'],
  'apps/worker/Dockerfile': ['apps/worker/', 'package.json', 'pnpm-lock.yaml', 'pnpm-workspace.yaml'],
};

const digestReference = /^registry\.cloudflare\.com\/[a-f0-9]{32}\/[a-z0-9][a-z0-9._-]*@sha256:[a-f0-9]{64}$/;

// parseJsonc reads wrangler's JSON with comments and trailing commas.
export function parseJsonc(text) {
  let out = '';
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (c === '"') {
      const start = i;
      for (i++; i < text.length && text[i] !== '"'; i++) if (text[i] === '\\') i++;
      out += text.slice(start, i + 1);
    } else if (c === '/' && text[i + 1] === '/') {
      while (i < text.length && text[i] !== '\n') i++;
      out += '\n';
    } else if (c === '/' && text[i + 1] === '*') {
      i = text.indexOf('*/', i + 2);
      if (i < 0) throw new Error('unterminated comment');
      i++;
    } else if (c !== ',' || !/^\s*[}\]]/.test(text.slice(i + 1).replace(/^(\s|\/\/[^\n]*|\/\*[\s\S]*?\*\/)*/, ''))) {
      out += c;
    }
  }
  return JSON.parse(out);
}

// images lists what config builds from Dockerfiles, under wrangler's own repository names
// (<worker>-<class>, <worker>-<class>-<image name>), so pushes reuse the layers already there.
// Paths are relative to configDir for the Dockerfile and context, to rootDir for the inputs.
export function images(config, configDir, rootDir) {
  const list = [];
  const add = (repository, dockerfile, context) => {
    const file = path.relative(rootDir, path.resolve(configDir, dockerfile));
    const paths = inputs[file];
    if (!paths) throw new Error(`no inputs for ${file}: add them to scripts/images.mjs`);
    list.push({ repository, dockerfile, context: context ?? '.', inputs: paths });
  };
  for (const c of config.containers ?? []) {
    const cls = c.class_name.toLowerCase();
    if (typeof c.image === 'string' && isDockerfile(c.image))
      add(`${config.name}-${cls}`, c.image, c.image_build_context);
    for (const [name, image] of Object.entries(c.images ?? {})) {
      if (image.dockerfile) add(`${config.name}-${cls}-${name}`, image.dockerfile, image.build_context);
    }
  }
  return list;
}

// deployConfig is config with each Dockerfile image replaced by refs[repository], a
// registry.cloudflare.com/<account>/<repository>@sha256:<digest> reference; nothing else changes.
export function deployConfig(config, refs) {
  const ref = (repository) => {
    const r = refs[repository];
    if (!r) throw new Error(`no image reference for ${repository}`);
    if (!digestReference.test(r) || !r.includes(`/${repository}@`)) {
      throw new Error(`${repository}: not a digest-pinned reference to its repository`);
    }
    return r;
  };
  const out = structuredClone(config);
  for (const c of out.containers ?? []) {
    const cls = c.class_name.toLowerCase();
    if (typeof c.image === 'string' && isDockerfile(c.image)) {
      c.image = ref(`${out.name}-${cls}`);
      delete c.image_build_context;
    }
    for (const [name, image] of Object.entries(c.images ?? {})) {
      if (image.dockerfile) c.images[name] = { image: ref(`${out.name}-${cls}-${name}`) };
    }
  }
  return out;
}

function isDockerfile(image) {
  return /(^|\/)Dockerfile[^/]*$/.test(image);
}

function main(args) {
  const configDir = process.cwd();
  const config = () => parseJsonc(readFileSync(path.join(configDir, 'wrangler.jsonc'), 'utf8'));
  const [command, ...rest] = args;
  if (command === 'list') {
    const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../..');
    for (const i of images(config(), configDir, rootDir)) {
      process.stdout.write(`${[i.repository, i.dockerfile, i.context, i.inputs.join(' ')].join('\t')}\n`);
    }
  } else if (command === 'config') {
    const refs = Object.fromEntries(rest.map((a) => [a.slice(0, a.indexOf('=')), a.slice(a.indexOf('=') + 1)]));
    process.stdout.write(
      `// Generated by scripts/deploy.sh from wrangler.jsonc for wrangler deploy; never edit or commit it.\n${JSON.stringify(deployConfig(config(), refs), null, 2)}\n`,
    );
  } else if (command === 'field') {
    const text = readFileSync(0, 'utf8');
    let value = JSON.parse(text.slice(text.indexOf('{')));
    for (const key of rest[0].split('/')) value = value?.[key];
    if (typeof value !== 'string' || value === '') throw new Error(`no field ${rest[0]}`);
    process.stdout.write(value);
  } else {
    throw new Error('usage: images.mjs list | config <repository>=<ref>... | field <path>');
  }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) main(process.argv.slice(2));

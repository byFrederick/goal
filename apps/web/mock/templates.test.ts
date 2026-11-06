import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { parse } from 'yaml';
import { seedTemplates } from './definition-seed.ts';

// The committed starter templates (ADR 0042) are the product's content; the mock seeds copies of them.
// A change to one must change the other, and this test fails until it does.
const root = fileURLToPath(new URL('../../../deploy/catalog/templates', import.meta.url));
const read = (path: string) => parse(readFileSync(path, 'utf8')) as Record<string, any>;
const decode = (b: Uint8Array | undefined) => new TextDecoder().decode(b);
const stationNames = (factory: Record<string, any>): string[] =>
  factory.spec.board.stations.map((s: { name: string }) => s.name);

const committed = readdirSync(root, { withFileTypes: true })
  .filter((d) => d.isDirectory())
  .flatMap((d) =>
    readdirSync(join(root, d.name), { withFileTypes: true })
      .filter((v) => v.isDirectory())
      .map((v) => ({ name: d.name, version: Number(v.name), dir: join(root, d.name, v.name) })),
  );

describe('the mock templates', () => {
  it('finds the committed templates', () => {
    expect(committed.map((c) => c.name)).toEqual(
      expect.arrayContaining(['software-delivery', 'incident-response', 'support-replies']),
    );
  });

  it.each(committed)('keeps $name v$version in step with deploy/catalog/templates', ({ name, version, dir }) => {
    const meta = read(join(dir, 'template.yaml'));
    const factory = read(join(dir, 'factory', 'factory.yaml'));
    const entry = seedTemplates().find((t) => t.name === name && t.version === version);
    expect(entry, `the mock has no ${name} v${version}`).toBeDefined();
    if (!entry) return;
    expect(entry.title).toBe(meta.title);
    expect(entry.summary).toBe(meta.summary);
    expect(entry.code).toBe(meta.code);
    const mock = parse(decode(entry.files.get('factory.yaml'))) as Record<string, any>;
    expect(stationNames(mock)).toEqual(stationNames(factory));
  });
});

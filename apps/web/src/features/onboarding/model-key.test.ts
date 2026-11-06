import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { parse } from 'yaml';
import { modelKeyOf } from '../factory-editor/basics/model-key';
import { withModelKey } from './model-key';

const templates = resolve(__dirname, '../../../../../deploy/catalog/templates');

describe('withModelKey', () => {
  for (const name of ['software-delivery', 'support-replies', 'incident-response']) {
    it(`sets runtime.modelKey in the ${name} template and keeps everything else`, () => {
      const text = readFileSync(resolve(templates, name, '1/factory/factory.yaml'), 'utf8');
      const next = withModelKey(text, 'model-key');
      expect(modelKeyOf(next)).toBe('model-key');
      expect(parse(next).spec.runtime.modelKey).toBe('{{model-key}}');
      expect(parse(next).spec.agents).toEqual(parse(text).spec.agents);
      expect(next.match(/^\s*#/gm)).toEqual(text.match(/^\s*#/gm));
    });
  }

  it('creates the runtime map when the file has none, and replaces an earlier reference', () => {
    expect(modelKeyOf(withModelKey('spec:\n  agents: []\n', 'key-a'))).toBe('key-a');
    expect(modelKeyOf(withModelKey(withModelKey('spec:\n  runtime: {}\n', 'key-a'), 'key-b'))).toBe('key-b');
  });
});

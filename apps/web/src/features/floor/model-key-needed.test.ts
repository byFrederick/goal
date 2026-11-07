import { describe, expect, it } from 'vitest';
import { needsModelKey, onSecrets } from './model-key-needed';

const withKey = 'spec:\n  runtime:\n    modelKey: "{{secret.anthropic}}"\n';
const withoutKey = 'spec:\n  runtime:\n    image: x\n';

describe('needsModelKey', () => {
  it('is true for an environment file and no model key', () => {
    expect(needsModelKey(['factory.yaml', 'environments/default.yaml'], withoutKey)).toBe(true);
  });
  it('is false once the model key is set', () => {
    expect(needsModelKey(['factory.yaml', 'environments/default.yaml'], withKey)).toBe(false);
  });
  it('is false with no environment files and no key', () => {
    expect(needsModelKey(['factory.yaml'], withoutKey)).toBe(false);
  });
});

describe('onSecrets', () => {
  it('is true with an environment file or a model key, false with neither', () => {
    expect(onSecrets(['environments/a.yaml'], withoutKey)).toBe(true);
    expect(onSecrets(['factory.yaml'], withKey)).toBe(true);
    expect(onSecrets(['factory.yaml'], withoutKey)).toBe(false);
  });
});

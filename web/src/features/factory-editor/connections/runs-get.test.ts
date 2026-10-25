import { describe, expect, it } from 'vitest';
import type { ConnectionKind } from '../../../api/types';
import { runsGetSummary } from './runs-get';

const kind = (mint?: string): ConnectionKind => ({
  name: 'k',
  title: 'K',
  platform: true,
  fields: [],
  modes: ['env'],
  model: false,
  testable: false,
  env: [],
  ...(mint ? { mint } : {}),
});

describe('runsGetSummary', () => {
  it('shows the first variable and how many more', () => {
    expect(runsGetSummary(kind(), ['GH_HOST', 'GH_TOKEN'])).toEqual({ names: ['GH_HOST'], more: 1, minted: false });
    expect(runsGetSummary(kind(), [])).toEqual({ names: [], more: 0, minted: false });
  });

  it('shows minted credentials by their shared prefix', () => {
    expect(runsGetSummary(kind('aws-sts'), ['AWS_ACCESS_KEY_ID', 'AWS_REGION', 'AWS_SESSION_TOKEN'])).toEqual({
      names: ['AWS_*'],
      more: 0,
      minted: true,
    });
  });
});

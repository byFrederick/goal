// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { Route as FloorRoute } from './o.$org.f.$factory.index';
import { Route as ItemRoute } from './o.$org.f.$factory.items.$ref';

type Head = (arg: unknown) => { meta: { title: string }[] };
const context = { factory: { name: 'support-replies', title: 'Support replies' }, org: { name: 'Acme' } };

describe('a factory route head', () => {
  it('shows the factory title on the Floor', () => {
    const head = FloorRoute.options.head as unknown as Head;
    expect(head({ match: { params: {}, context } }).meta[0]?.title).toBe('Floor · Support replies · Acme · Goal');
  });

  it('keeps the factory name in the item key', () => {
    const head = ItemRoute.options.head as unknown as Head;
    expect(head({ match: { context }, params: { ref: '12' } }).meta[0]?.title).toBe(
      'support-replies#12 · Support replies · Acme · Goal',
    );
  });
});

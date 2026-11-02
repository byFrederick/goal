// @vitest-environment jsdom
import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { FactoryBadge } from './factory-badge';

afterEach(cleanup);

describe('FactoryBadge', () => {
  it('is always an 18 px square with a 4 px radius', () => {
    const { container } = render(<FactoryBadge mark="A" />);
    const badge = container.firstElementChild as HTMLElement;
    expect(badge.textContent).toBe('A');
    expect(Array.from(badge.classList)).toEqual(expect.arrayContaining(['size-[18px]', 'rounded-chip', 'bg-run']));
  });
});

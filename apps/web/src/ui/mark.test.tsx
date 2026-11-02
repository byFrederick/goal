// @vitest-environment jsdom
import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { FactoryBadge } from '../app/factory-badge';
import { Mark } from './mark';

afterEach(cleanup);

describe('Mark', () => {
  it('draws its count as an orange 18 px pill', () => {
    const { container } = render(
      <Mark kind="factory" badge={4}>
        A
      </Mark>,
    );
    const pill = container.querySelector('[data-count="4"]') as HTMLElement;
    expect(Array.from(pill.classList)).toEqual(
      expect.arrayContaining(['h-[18px]', 'min-w-[18px]', 'rounded-full', 'bg-you', 'text-white']),
    );
    expect(pill.classList).not.toContain('rounded-chip');
  });

  it('holds the 18 px factory badge inside the 38 px square', () => {
    const { container } = render(
      <Mark kind="factory">
        <FactoryBadge mark="A" />
      </Mark>,
    );
    const square = container.firstElementChild as HTMLElement;
    expect(square.classList).toContain('size-[38px]');
    expect((square.firstElementChild as HTMLElement).classList).toContain('size-[18px]');
  });

  it('draws no count at zero', () => {
    const { container } = render(<Mark kind="factory">A</Mark>);
    expect(container.querySelector('[data-count]')).toBeNull();
  });
});

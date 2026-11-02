// @vitest-environment jsdom
import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
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

  it('draws no count at zero', () => {
    const { container } = render(<Mark kind="factory">A</Mark>);
    expect(container.querySelector('[data-count]')).toBeNull();
  });
});

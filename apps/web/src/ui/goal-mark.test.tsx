// @vitest-environment jsdom
import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { GoalMark } from './goal-mark';

afterEach(cleanup);

describe('GoalMark', () => {
  it('is hidden from assistive technology, so the link around it holds the name', () => {
    const { container } = render(<GoalMark />);
    const square = container.firstElementChild as HTMLElement;
    expect(square.getAttribute('aria-hidden')).toBe('true');
    expect(square.classList.contains('bg-run')).toBe(true);
    expect(square.classList.contains('rounded-control')).toBe(true);
  });

  it('draws the G arc and the yellow node from the shell and sign-up HTML', () => {
    const { container } = render(<GoalMark />);
    const svg = container.querySelector('svg') as SVGElement;
    expect(svg.getAttribute('viewBox')).toBe('0 0 24 24');

    const path = container.querySelector('path') as SVGElement;
    expect(path.getAttribute('d')).toBe('M18.6 6.9A8.2 8.2 0 1 0 20.2 12H13');
    expect(path.getAttribute('fill')).toBe('none');
    expect(path.getAttribute('stroke-width')).toBe('3.2');
    expect(path.getAttribute('stroke-linecap')).toBe('round');
    expect(path.classList.contains('stroke-white')).toBe(true);

    const node = container.querySelector('circle') as SVGElement;
    expect(node.getAttribute('cx')).toBe('12.4');
    expect(node.getAttribute('cy')).toBe('12');
    expect(node.getAttribute('r')).toBe('2.9');
    expect(node.classList.contains('fill-amber')).toBe(true);
  });

  it('uses the sidebar size by default', () => {
    const { container } = render(<GoalMark />);
    expect((container.firstElementChild as HTMLElement).classList.contains('size-7')).toBe(true);
    expect(container.querySelector('svg')?.classList.contains('size-4')).toBe(true);
  });

  it('uses the sign-up size for the brand', () => {
    const { container } = render(<GoalMark size="brand" />);
    expect((container.firstElementChild as HTMLElement).classList.contains('size-8')).toBe(true);
    expect(container.querySelector('svg')?.classList.contains('size-5')).toBe(true);
  });
});

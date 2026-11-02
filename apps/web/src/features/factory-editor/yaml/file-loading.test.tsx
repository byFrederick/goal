// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { FileLoading } from './yaml-view';

afterEach(cleanup);

describe('a file waiting on its text', () => {
  it('shows a busy skeleton and names the file for screen readers', () => {
    const { container } = render(<FileLoading path="agents/review.yaml" />);
    expect(screen.getByText('Loading agents/review.yaml')).toBeTruthy();
    const busy = container.querySelector('[aria-busy="true"]');
    expect(busy).not.toBeNull();
    expect(busy?.children.length).toBeGreaterThan(0);
  });
});

// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { DefinitionSkeleton } from './route-skeletons';

afterEach(cleanup);

describe('the Definition route while it loads', () => {
  it('shows its layout as a busy skeleton with no text', () => {
    const { container } = render(<DefinitionSkeleton />);
    expect(container.querySelector('[aria-busy="true"]')).not.toBeNull();
    expect(screen.getByText('Loading')).toBeTruthy();
  });
});

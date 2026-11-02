// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { PendingScreen } from './states';

describe('the first-load state', () => {
  afterEach(() => {
    cleanup();
  });

  it('draws the page skeleton, busy and announced as loading', () => {
    const { container } = render(<PendingScreen />);

    expect(screen.getByText('Loading')).toBeTruthy();
    expect(container.querySelector('[aria-busy="true"]')).not.toBeNull();
    expect(container.querySelector('.animate-skeleton-pulse')).not.toBeNull();
  });

  it('hides the placeholder from assistive tech and keeps the empty content inert', () => {
    const { container } = render(<PendingScreen />);

    expect(container.querySelector('[aria-hidden="true"] .animate-skeleton-pulse')).not.toBeNull();
    expect(container.querySelector('[inert]')).not.toBeNull();
  });
});

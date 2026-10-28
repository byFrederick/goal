// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { DraftBanner } from './draft-banner';
import { DraftFailed, DraftLoading } from './draft-states';

afterEach(cleanup);

describe('the Definition states', () => {
  it('loading: the app background with an sr-only "Loading" and aria-busy, no placeholder shapes', () => {
    const { container } = render(<DraftLoading />);
    const busy = container.querySelector('[aria-busy="true"]');
    expect(busy?.textContent).toBe('Loading');
    expect(busy?.querySelectorAll('div').length).toBe(0);
  });

  it('error: says the draft could not load and offers Try again', () => {
    const onRetry = vi.fn();
    render(<DraftFailed detail="goald is not answering." onRetry={onRetry} />);
    expect(screen.getByRole('alert').textContent).toContain('The draft could not load');
    expect(screen.getByText('goald is not answering.')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(onRetry).toHaveBeenCalledOnce();
  });

  it('ready: the banner says the draft, its problems and what runs', () => {
    render(
      <DraftBanner next={22} current={21} changeCount={3} problemCount={1}>
        <button type="button">Review changes</button>
      </DraftBanner>,
    );
    const banner = screen.getByRole('region', { name: 'Draft' });
    expect(banner.textContent).toContain('Draft of v22, 3 changes');
    expect(banner.textContent).toContain('Fix 1 problem before you publish. Running items keep v21.');
    expect(screen.getByRole('button', { name: 'Review changes' })).toBeTruthy();
  });

  it('ready without problems: no problem sentence', () => {
    render(
      <DraftBanner next={9} current={null} changeCount={1} problemCount={0}>
        <span>actions</span>
      </DraftBanner>,
    );
    const text = screen.getByRole('region', { name: 'Draft' }).textContent ?? '';
    expect(text).toContain('Draft of v9, 1 change');
    expect(text).not.toContain('before you publish');
    expect(text).not.toContain('Running items keep');
  });
});

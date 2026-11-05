// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { PageActions } from './page-actions';

vi.mock('@tanstack/react-router', () => ({
  Link: ({ children, to: _to, params: _params, ...rest }: { children: ReactNode; to: string; params: unknown }) => (
    <a href="/x" {...rest}>
      {children}
    </a>
  ),
}));
vi.mock('../../../shared/factory', () => ({ useFactoryParams: () => ({ org: 'acme', factory: 'f' }) }));
vi.mock('./view-toggle', () => ({ ViewToggle: () => <div role="radiogroup" aria-label="Editor view" /> }));

function setNarrow(narrow: boolean) {
  window.matchMedia = vi.fn().mockImplementation(() => ({
    matches: narrow,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  })) as never;
}

afterEach(() => {
  cleanup();
  // @ts-expect-error restoring the jsdom default, which has no matchMedia
  delete window.matchMedia;
});

const props = {
  section: 'line' as const,
  yaml: false,
  viewOnly: false,
  canPublish: false,
  publishNumber: 9,
  describedBy: 'why',
  reviewDescribedBy: 'none-yet',
};

describe('PageActions on a phone', () => {
  it('is one row: Versions is only in the More menu, which is a menu button', () => {
    setNarrow(true);
    render(<PageActions {...props} />);
    expect(screen.queryByRole('link', { name: 'Versions' })).toBeNull();
    const more = screen.getByRole('button', { name: 'More actions' });
    expect(more.getAttribute('aria-haspopup')).toBe('menu');
    expect(more.className).toContain('justify-center');
    expect(screen.getByRole('radiogroup', { name: 'Editor view' })).toBeTruthy();
  });

  it('keeps "No changes yet" on Review changes while nothing has changed', () => {
    setNarrow(true);
    render(<PageActions {...props} />);
    expect(screen.getByRole('link', { name: 'Review changes' }).getAttribute('aria-describedby')).toBe('none-yet');
  });

  it('puts Versions and, when Publish is primary, Review changes in the menu', () => {
    setNarrow(true);
    render(<PageActions {...props} canPublish reviewDescribedBy={undefined} />);
    expect(screen.getByRole('link', { name: 'Publish v9' })).toBeTruthy();
    fireEvent.pointerDown(screen.getByRole('button', { name: 'More actions' }), { button: 0, pointerType: 'mouse' });
    fireEvent.click(screen.getByRole('button', { name: 'More actions' }));
    expect(screen.getAllByRole('menuitem').map((i) => i.textContent)).toEqual(['Versions', 'Review changes']);
  });
});

describe('PageActions on Review and publish', () => {
  it('leaves Publish to the page, so there is one Publish v9 button', () => {
    for (const narrow of [false, true]) {
      setNarrow(narrow);
      const { unmount } = render(<PageActions {...props} section="review" canPublish />);
      expect(screen.queryByRole('link', { name: 'Publish v9' })).toBeNull();
      expect(screen.queryByRole('button', { name: 'Publish v9' })).toBeNull();
      unmount();
    }
  });
});

describe('PageActions on wider screens', () => {
  it('lists Versions beside the other actions', () => {
    setNarrow(false);
    render(<PageActions {...props} />);
    expect(screen.getByRole('link', { name: 'Versions' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'More actions' })).toBeNull();
  });

  it('switches to the narrow row below 1280 px, where the full row does not fit beside the crumbs', () => {
    setNarrow(false);
    render(<PageActions {...props} />);
    expect(window.matchMedia).toHaveBeenCalledWith('(max-width: 1279px)');
  });
});

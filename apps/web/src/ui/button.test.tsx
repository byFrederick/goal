// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { Button } from './button';

afterEach(cleanup);

const classes = (name: string) => Array.from(screen.getByRole('button', { name }).classList);

describe('Button', () => {
  it('draws the primary action as the near-black action fill with its ink text', () => {
    render(<Button variant="primary">Save</Button>);
    expect(classes('Save')).toEqual(
      expect.arrayContaining([
        'bg-action',
        'border-action',
        'text-action-ink',
        'h-8',
        'rounded-control',
        'font-control',
      ]),
    );
    expect(classes('Save')).not.toContain('bg-run');
  });

  it('draws the ink action with the action tokens, not cobalt', () => {
    render(<Button variant="action">Save changes</Button>);
    const list = classes('Save changes');
    expect(list).toEqual(expect.arrayContaining(['bg-action', 'text-action-ink']));
    expect(list).not.toContain('bg-run');
  });

  it('draws the action variant in ink, never cobalt', () => {
    render(<Button variant="action">Invite</Button>);
    const c = classes('Invite');
    expect(c).toEqual(expect.arrayContaining(['bg-action', 'text-action-ink', 'border-action']));
    expect(c).not.toContain('bg-run');
  });

  it('draws the secondary action white with a rule border, and keeps the old names as aliases', () => {
    const { unmount } = render(<Button variant="secondary">Edit</Button>);
    expect(classes('Edit')).toEqual(expect.arrayContaining(['bg-card', 'border-rule', 'text-ink']));
    unmount();

    for (const alias of ['raised', 'white'] as const) {
      const { unmount: done } = render(<Button variant={alias}>{alias}</Button>);
      expect(classes(alias)).toEqual(expect.arrayContaining(['bg-card', 'border-rule']));
      done();
    }
  });

  it('draws danger as red text and danger-fill as a red fill', () => {
    const { unmount } = render(<Button variant="danger">Remove</Button>);
    expect(classes('Remove')).toEqual(expect.arrayContaining(['text-rework-ink']));
    expect(classes('Remove')).not.toContain('bg-rework');
    unmount();

    render(<Button variant="danger-fill">Delete for good</Button>);
    expect(classes('Delete for good')).toEqual(expect.arrayContaining(['bg-rework', 'text-white']));
  });

  it('maps the rework alias to danger, so a caller keeps its red text', () => {
    render(<Button variant="rework">Send back</Button>);
    expect(classes('Send back')).toEqual(expect.arrayContaining(['text-rework-ink']));
  });

  it('draws approve as a violet fill', () => {
    render(<Button variant="approve">Approve</Button>);
    expect(classes('Approve')).toEqual(expect.arrayContaining(['bg-you', 'text-white']));
  });

  it('sizes the small button at 28 px and the icon button at 32 px', () => {
    const { unmount } = render(<Button size="sm">Small</Button>);
    expect(classes('Small')).toContain('h-7');
    unmount();

    render(
      <Button size="icon" aria-label="Close">
        <span aria-hidden="true">x</span>
      </Button>,
    );
    expect(classes('Close')).toContain('size-8');
  });

  it('presses with the press utility, and has no ad-hoc duration', () => {
    for (const size of ['md', 'icon'] as const) {
      const { unmount } = render(
        size === 'icon' ? (
          <Button size="icon" aria-label={`Press ${size}`}>
            <span aria-hidden="true">x</span>
          </Button>
        ) : (
          <Button>{`Press ${size}`}</Button>
        ),
      );
      const names = classes(`Press ${size}`);
      expect(names).toContain('press');
      expect(names).not.toContain('duration-200');
      unmount();
    }
  });

  it('gives an icon button its accessible name from aria-label', () => {
    render(
      <Button size="icon" aria-label="Actions for Ada">
        <span aria-hidden="true">…</span>
      </Button>,
    );
    expect(screen.getByRole('button', { name: 'Actions for Ada' })).toBeTruthy();
  });

  it('does not type-check an icon button without aria-label', () => {
    // The type is the test: this file fails to typecheck if the icon size loses its required name.
    // @ts-expect-error an icon button has no accessible name
    const unnamed = () => <Button size="icon" />;
    expect(typeof unnamed).toBe('function');
  });

  // jsdom does not apply Tailwind's CSS, so these assert classes, not computed heights.
  const variantNames = [
    'primary',
    'action',
    'secondary',
    'raised',
    'white',
    'danger',
    'danger-fill',
    'approve',
    'rework',
    'ghost',
  ] as const;
  const desktop = { sm: 'h-7', md: 'h-8', lg: 'h-11' } as const;

  describe.each(variantNames)('variant %s', (variant) => {
    it.each(['sm', 'md'] as const)('keeps the %s desktop height and is at least 44 px below md', (size) => {
      render(
        <Button variant={variant} size={size}>
          Go
        </Button>,
      );
      expect(classes('Go')).toEqual(expect.arrayContaining([desktop[size], 'max-md:min-h-11']));
    });

    it('keeps lg at 44 px with no phone override', () => {
      render(
        <Button variant={variant} size="lg">
          Go
        </Button>,
      );
      expect(classes('Go')).toContain('h-11');
      expect(classes('Go')).not.toContain('max-md:min-h-11');
    });

    it('keeps icon at 32 px with its coarse-pointer hit area', () => {
      render(<Button variant={variant} size="icon" aria-label="Go" />);
      expect(classes('Go')).toEqual(
        expect.arrayContaining(['size-8', 'pointer-coarse:before:absolute', 'pointer-coarse:before:-inset-2']),
      );
      expect(classes('Go')).not.toContain('max-md:min-h-11');
    });
  });
});

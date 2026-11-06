// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Button } from './button';
import { SuccessLabel } from './success-label';

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

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

  describe('busy', () => {
    const slot = (name: 'idle' | 'busy') => document.querySelector(`[data-slot="${name}"]`) as HTMLElement;

    it('shows the busy label and spinner only after 100 ms', () => {
      vi.useFakeTimers();
      render(
        <Button busy busyLabel="Saving…">
          Save
        </Button>,
      );
      expect(slot('busy').className).toContain('invisible');
      expect(slot('busy').getAttribute('aria-hidden')).toBe('true');
      act(() => {
        vi.advanceTimersByTime(99);
      });
      expect(slot('busy').className).toContain('invisible');
      act(() => {
        vi.advanceTimersByTime(1);
      });
      expect(slot('busy').className).not.toContain('invisible');
      expect(slot('busy').className).toContain('motion-safe:animate-busy-swap');
      expect(slot('idle').className).toContain('invisible');
      expect(slot('busy').querySelector('.animate-spin')).not.toBeNull();
      expect(screen.getByRole('button', { name: 'Saving…' })).toBeTruthy();
    });

    it('never shows the busy label when busy ends before 100 ms', () => {
      vi.useFakeTimers();
      const { rerender } = render(
        <Button busy busyLabel="Saving…">
          Save
        </Button>,
      );
      act(() => {
        vi.advanceTimersByTime(60);
      });
      rerender(
        <Button busy={false} busyLabel="Saving…">
          Save
        </Button>,
      );
      act(() => {
        vi.advanceTimersByTime(200);
      });
      expect(slot('busy').className).toContain('invisible');
      expect(screen.getByRole('button', { name: 'Save' })).toBeTruthy();
    });

    it('sets aria-busy and aria-disabled, not disabled, and keeps focus', () => {
      const { rerender } = render(<Button busyLabel="Saving…">Save</Button>);
      const button = screen.getByRole('button', { name: 'Save' });
      button.focus();
      rerender(
        <Button busy busyLabel="Saving…">
          Save
        </Button>,
      );
      expect(button.getAttribute('aria-busy')).toBe('true');
      expect(button.getAttribute('aria-disabled')).toBe('true');
      expect(button.hasAttribute('disabled')).toBe(false);
      expect(document.activeElement).toBe(button);
      expect(Array.from(button.classList)).toContain('cursor-progress');
      expect(Array.from(button.classList)).not.toContain('aria-disabled:opacity-55');
    });

    it('ignores clicks and the submit of a form while busy', () => {
      const onClick = vi.fn();
      const onSubmit = vi.fn((event: { preventDefault: () => void }) => event.preventDefault());
      render(
        <form onSubmit={onSubmit}>
          <Button type="submit" busy busyLabel="Saving…" onClick={onClick}>
            Save
          </Button>
        </form>,
      );
      fireEvent.click(screen.getByRole('button'));
      expect(onClick).not.toHaveBeenCalled();
      expect(onSubmit).not.toHaveBeenCalled();
    });

    it('still calls onClick and submits when not busy', () => {
      const onClick = vi.fn();
      render(
        <Button busy={false} busyLabel="Saving…" onClick={onClick}>
          Save
        </Button>,
      );
      fireEvent.click(screen.getByRole('button'));
      expect(onClick).toHaveBeenCalledTimes(1);
    });

    it('renders both labels in one grid cell, the inactive one invisible', () => {
      render(
        <Button busy busyLabel="Publishing v41…">
          Publish
        </Button>,
      );
      for (const name of ['idle', 'busy'] as const) {
        expect(slot(name).className).toContain('col-start-1');
        expect(slot(name).className).toContain('row-start-1');
      }
      expect(slot('idle').parentElement?.className).toContain('inline-grid');
      expect(slot('busy').className).toContain('invisible');
      expect(slot('idle').className).not.toContain('invisible');
    });

    it('picks the spinner tone from the variant and keeps it still under reduced motion', () => {
      const tone = (variant: 'primary' | 'approve' | 'danger-fill' | 'secondary' | 'ghost' | 'danger') => {
        const { unmount } = render(
          <Button variant={variant} busy busyLabel="Working…">
            Go
          </Button>,
        );
        const spinner = slot('busy').querySelector('.animate-spin') as HTMLElement;
        const names = Array.from(spinner.classList);
        unmount();
        return names;
      };
      for (const v of ['primary', 'approve', 'danger-fill'] as const) {
        expect(tone(v)).toContain('border-t-action-ink');
      }
      for (const v of ['secondary', 'ghost', 'danger'] as const) {
        expect(tone(v)).toContain('border-t-ink');
      }
      expect(tone('primary')).toContain('motion-reduce:animate-none');
    });

    it('composes with SuccessLabel: both labels sit in the grid cell', () => {
      render(
        <Button busy busyLabel="Approving…">
          <SuccessLabel done={false} doneLabel="Approved">
            Approve
          </SuccessLabel>
        </Button>,
      );
      expect(slot('idle').textContent).toBe('Approve');
      expect(slot('busy').textContent).toBe('Approving…');
      expect(slot('idle').parentElement).toBe(slot('busy').parentElement);
    });
  });
});

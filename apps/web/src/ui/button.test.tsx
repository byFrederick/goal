// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { Button } from './button';

afterEach(cleanup);

const classes = (name: string) => Array.from(screen.getByRole('button', { name }).classList);

describe('Button', () => {
  it('draws the primary action in cobalt with white text', () => {
    render(<Button variant="primary">Save</Button>);
    expect(classes('Save')).toEqual(
      expect.arrayContaining(['bg-run', 'text-white', 'h-8.5', 'rounded-control', 'font-control']),
    );
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

  it('sizes the small button at 30 px and the icon button at 32 px', () => {
    const { unmount } = render(<Button size="sm">Small</Button>);
    expect(classes('Small')).toContain('h-7.5');
    unmount();

    render(
      <Button size="icon" aria-label="Close">
        <span aria-hidden="true">x</span>
      </Button>,
    );
    expect(classes('Close')).toContain('size-8');
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
});

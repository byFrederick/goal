// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Disclosure } from './disclosure';

afterEach(cleanup);

function details(props: Partial<Parameters<typeof Disclosure>[0]> = {}) {
  return (
    <Disclosure summary="Tool call details" {...props}>
      <button type="button">Copy output</button>
    </Disclosure>
  );
}

const summaryOf = () => screen.getByRole('button', { name: 'Tool call details' });

describe('Disclosure', () => {
  it('hides its content until its summary is pressed, and hides it again on a second press', () => {
    render(details());

    expect(summaryOf().getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByRole('button', { name: 'Copy output' })).toBeNull();

    fireEvent.click(summaryOf());
    expect(summaryOf().getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByRole('button', { name: 'Copy output' })).toBeTruthy();

    fireEvent.click(summaryOf());
    expect(summaryOf().getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByRole('button', { name: 'Copy output' })).toBeNull();
  });

  it('names the content it controls, so the summary tells assistive technology what it opens', () => {
    render(details());
    const controlled = document.getElementById(summaryOf().getAttribute('aria-controls') ?? '');

    expect(controlled).not.toBeNull();
    expect(controlled?.contains(screen.getByRole('button', { name: 'Copy output', hidden: true }))).toBe(true);
  });

  it('starts open when defaultOpen is set', () => {
    render(details({ defaultOpen: true }));

    expect(summaryOf().getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByRole('button', { name: 'Copy output' })).toBeTruthy();
  });

  it('follows a controlled open state and reports the change it asks for', () => {
    const onOpenChange = vi.fn();
    render(details({ open: false, onOpenChange }));

    fireEvent.click(summaryOf());

    expect(onOpenChange).toHaveBeenCalledWith(true);
    // The parent owns the state: still closed until it passes open back in.
    expect(summaryOf().getAttribute('aria-expanded')).toBe('false');
  });
});

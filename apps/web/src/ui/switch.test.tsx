// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Switch } from './switch';

afterEach(cleanup);

function Toggle({ disabled = false, onChange }: { disabled?: boolean; onChange?: (on: boolean) => void }) {
  const [on, setOn] = useState(false);
  return (
    <Switch
      aria-label="Notify me"
      checked={on}
      disabled={disabled}
      onCheckedChange={(next) => {
        setOn(next);
        onChange?.(next);
      }}
    />
  );
}

describe('Switch', () => {
  it('is a switch named by its label', () => {
    render(<Toggle />);
    expect(screen.getByRole('switch', { name: 'Notify me' }).getAttribute('aria-checked')).toBe('false');
  });

  it('toggles when clicked', () => {
    const onChange = vi.fn();
    render(<Toggle onChange={onChange} />);
    const sw = screen.getByRole('switch', { name: 'Notify me' });
    fireEvent.click(sw);
    expect(sw.getAttribute('aria-checked')).toBe('true');
    expect(onChange).toHaveBeenCalledWith(true);
  });

  it('does not toggle when disabled, and stays focusable with aria-disabled', () => {
    const onChange = vi.fn();
    render(<Toggle disabled onChange={onChange} />);
    const sw = screen.getByRole('switch', { name: 'Notify me' });
    expect(sw.getAttribute('aria-disabled')).toBe('true');
    fireEvent.click(sw);
    expect(sw.getAttribute('aria-checked')).toBe('false');
    expect(onChange).not.toHaveBeenCalled();
  });
});

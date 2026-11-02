// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
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
  it('answers across 44 px or more below 768 px: its 22 px inner box plus 12 px each side', () => {
    render(<Toggle />);
    expect(screen.getByRole('switch').className).toContain('before:-inset-3');
  });

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

  it('toggles with the Space key', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<Toggle onChange={onChange} />);
    const sw = screen.getByRole('switch', { name: 'Notify me' });
    sw.focus();
    await user.keyboard(' ');
    expect(sw.getAttribute('aria-checked')).toBe('true');
    expect(onChange).toHaveBeenCalledWith(true);
  });

  it('travels the thumb at the medium bounce and fades the track at the quick duration', () => {
    const { container } = render(<Toggle />);
    const track = screen.getByRole('switch', { name: 'Notify me' });
    const thumb = container.querySelector('[aria-hidden="true"]');
    expect(Array.from(thumb?.classList ?? [])).toEqual(
      expect.arrayContaining(['transition-transform', 'duration-(--duration-medium)', 'ease-bounce']),
    );
    expect(Array.from(track.classList)).toEqual(
      expect.arrayContaining(['transition-colors', 'duration-(--duration-quick)']),
    );
  });

  it('does not toggle when disabled, and stays focusable with aria-disabled', async () => {
    const onChange = vi.fn();
    render(<Toggle disabled onChange={onChange} />);
    const sw = screen.getByRole('switch', { name: 'Notify me' });
    expect(sw.getAttribute('aria-disabled')).toBe('true');
    fireEvent.click(sw);
    sw.focus();
    await userEvent.keyboard(' ');
    expect(sw.getAttribute('aria-checked')).toBe('false');
    expect(onChange).not.toHaveBeenCalled();
  });
});

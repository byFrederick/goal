// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { Skeleton } from './skeleton';

afterEach(cleanup);

function Card({ ready }: { ready: boolean }) {
  return (
    <Skeleton ready={ready} placeholder={<span>Placeholder bar</span>}>
      <p>Loaded title</p>
    </Skeleton>
  );
}

describe('Skeleton', () => {
  it('is busy, with a Loading label for a screen reader, while it waits', () => {
    const { container } = render(<Card ready={false} />);
    expect(container.firstElementChild?.getAttribute('aria-busy')).toBe('true');
    expect(screen.getByText('Loading')).toBeTruthy();
  });

  it('pulses its placeholder once, at the pulse duration', () => {
    render(<Card ready={false} />);
    const pulse = screen.getByText('Placeholder bar').parentElement;
    expect(Array.from(pulse?.classList ?? [])).toContain('animate-skeleton-pulse');
  });

  it('keeps the content out of reach and out of the way until it is ready', () => {
    render(<Card ready={false} />);
    const content = screen.getByText('Loaded title').parentElement;
    expect(content?.hasAttribute('inert')).toBe(true);
    expect(Array.from(content?.classList ?? [])).toEqual(
      expect.arrayContaining(['opacity-0', 'blur-(--blur-small)', 'duration-(--duration-slow)']),
    );
  });

  it('shows the content, not busy, once ready, with the placeholder faded out', () => {
    const { container, rerender } = render(<Card ready={false} />);
    rerender(<Card ready />);
    expect(container.firstElementChild?.hasAttribute('aria-busy')).toBe(false);
    expect(screen.queryByText('Loading')).toBeNull();
    const content = screen.getByText('Loaded title').parentElement;
    expect(content?.hasAttribute('inert')).toBe(false);
    expect(content?.classList).not.toContain('opacity-0');
    const placeholder = screen.getByText('Placeholder bar').parentElement?.parentElement;
    expect(Array.from(placeholder?.classList ?? [])).toEqual(
      expect.arrayContaining(['opacity-0', 'blur-(--blur-small)']),
    );
  });

  it('lets clicks through the faded placeholder once ready', () => {
    render(<Card ready />);
    const placeholder = screen.getByText('Placeholder bar').parentElement?.parentElement;
    expect(Array.from(placeholder?.classList ?? [])).toContain('pointer-events-none');
  });

  it('takes the faded placeholder out of flow once ready, so the content sets the height', () => {
    const { container, rerender } = render(<Card ready={false} />);
    const placeholder = () => screen.getByText('Placeholder bar').parentElement?.parentElement;
    expect(Array.from(placeholder()?.classList ?? [])).not.toContain('absolute');
    rerender(<Card ready />);
    expect(Array.from(placeholder()?.classList ?? [])).toEqual(
      expect.arrayContaining(['absolute', 'inset-0', 'overflow-hidden']),
    );
    expect(Array.from(container.firstElementChild?.classList ?? [])).toContain('relative');
  });
});

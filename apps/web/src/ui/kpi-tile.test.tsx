// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { Delta, KpiTile } from './kpi-tile';

afterEach(cleanup);

// The delta is one span whose text is the arrow and the signed figure together.
const deltaText = (text: string) => screen.getByText((_, el) => el?.localName === 'span' && el.textContent === text);

describe('Delta', () => {
  it('shows an up arrow and a plus sign for a rise', () => {
    render(<Delta value={2} unit="pts" good={false} />);
    expect(deltaText('▲ +2 pts')).toBeTruthy();
  });

  it('shows a down arrow and a true minus sign for a fall', () => {
    render(<Delta value={-0.6} good />);
    expect(deltaText('▼ −0.6')).toBeTruthy();
  });

  it('shows zero with no arrow', () => {
    const { container } = render(<Delta value={0} good />);
    expect(container.textContent).toBe('0');
  });

  it('keeps the text the same when good changes and only the colour moves', () => {
    const { container, rerender } = render(<Delta value={4} unit="h" good />);
    const good = container.textContent;
    expect(container.querySelector('span')?.className).toContain('text-ok-ink');
    rerender(<Delta value={4} unit="h" good={false} />);
    expect(container.textContent).toBe(good);
    expect(container.querySelector('span')?.className).toContain('text-rework-ink');
  });
});

describe('KpiTile', () => {
  it('names its bar as a meter with its value and range', () => {
    render(
      <KpiTile
        label="Throughput"
        value="7.6"
        unit="a day"
        bar={{ value: 7.6, max: 10, tone: 'run', label: '76% of the target of 10 a day' }}
        target="Target 10"
        delta={{ value: -0.6, unit: 'a day', good: false }}
      />,
    );
    const meter = screen.getByRole('meter', { name: '76% of the target of 10 a day' });
    expect(meter.getAttribute('aria-valuenow')).toBe('7.6');
    expect(meter.getAttribute('aria-valuemin')).toBe('0');
    expect(meter.getAttribute('aria-valuemax')).toBe('10');
    expect(deltaText('▼ −0.6 a day')).toBeTruthy();
  });

  it('draws a sparkline with its label instead of a bar', () => {
    render(
      <KpiTile
        label="Hands-off"
        value="82"
        unit="%"
        sparkline={{ points: [70, 74, 82], label: 'Rising from 70% to 82% over 3 days' }}
        target="No human touch"
      />,
    );
    expect(screen.getByRole('img', { name: 'Rising from 70% to 82% over 3 days' })).toBeTruthy();
    expect(screen.queryByRole('meter')).toBeNull();
  });
});

// @vitest-environment jsdom
import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { ConnectionGlyph } from './connection-glyph';

afterEach(cleanup);

describe('ConnectionGlyph', () => {
  it('draws the brand glyph in a bordered square for a kind that has one', () => {
    const { container } = render(<ConnectionGlyph kind="github" title="GitHub" />);
    expect(container.querySelector('svg')).not.toBeNull();
    expect(container.firstElementChild?.className).toContain('border-line');
    expect(container.firstElementChild?.className).not.toContain('bg-panel');
  });

  it('shows the uppercase initial of the title for a kind without a glyph', () => {
    const { container } = render(<ConnectionGlyph kind="datadog" title="datadog" />);
    expect(container.querySelector('svg')).toBeNull();
    expect(container.textContent).toBe('D');
  });
});

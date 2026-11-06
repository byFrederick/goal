// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { TooltipProvider } from '../ui/tooltip';
import { RepositoriesField } from './repositories-field';

afterEach(cleanup);

describe('RepositoriesField on a phone', () => {
  const long = 'acme-platform/a-repository-with-a-very-long-name-that-cannot-fit-in-390-pixels';

  it('gives each chip a 44 px Remove button and keeps its long name truncated, with the full name in title', () => {
    render(
      <TooltipProvider>
        <RepositoriesField
          choices={{ options: [], owners: ['acme-platform'], initial: undefined }}
          value={[long, 'acme/app']}
          onChange={() => undefined}
          factory="goal"
        />
      </TooltipProvider>,
    );
    for (const repo of [long, 'acme/app']) {
      const remove = screen.getByRole('button', { name: `Remove ${repo}` });
      expect(remove.className).toContain('max-md:size-11');
      expect(remove.parentElement?.className).toContain('max-md:h-11');
    }
    const name = screen.getByTitle(long);
    expect(name.className).toContain('truncate');
    expect(name.className).toContain('min-w-0');
    expect(screen.getByRole('button', { name: 'About Repositories' }).className).toContain('max-md:size-11');
  });
});

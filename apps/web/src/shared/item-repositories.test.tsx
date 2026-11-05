// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { ItemRepository } from '../api/types';
import { TooltipProvider } from '../ui/tooltip';
import { ItemRepositories, repoName, usesSeveralRepositories } from './item-repositories';

afterEach(cleanup);

const repos: ItemRepository[] = [
  { repo: 'github.com/acme/api', joined: 'default' },
  { repo: 'github.com/acme/docs', joined: 'filing' },
  { repo: 'github.com/acme/deploy', joined: 'push' },
  { repo: 'github.com/acme/web', joined: 'output' },
];

describe('ItemRepositories', () => {
  it('shows the first repository as owner/name with +N for the rest', () => {
    render(
      <TooltipProvider>
        <ItemRepositories item={{ ref: 'goal#1', repositories: repos }} />
      </TooltipProvider>,
    );
    const first = screen.getByText('acme/api');
    expect(first.className).toContain('font-mono');
    expect(first.className).toContain('truncate');
    expect(screen.getByText('+3')).toBeTruthy();
  });

  it('lists every repository and how it joined in the ⓘ', () => {
    render(
      <TooltipProvider>
        <ItemRepositories item={{ ref: 'goal#1', repositories: repos }} />
      </TooltipProvider>,
    );
    const button = screen.getByRole('button', { name: 'About goal#1 repositories' });
    fireEvent.click(button);
    const text = document.body.textContent ?? '';
    expect(text).toContain('acme/api: Factory default');
    expect(text).toContain('acme/docs: Named when filed');
    expect(text).toContain('acme/deploy: An agent pushed to it');
    expect(text).toContain('acme/web: A station’s output named it');
  });

  it('shows nothing for an item without repositories', () => {
    const { container } = render(<ItemRepositories item={{ ref: 'goal#1', repositories: [] }} />);
    expect(container.textContent).toBe('');
  });
});

describe('usesSeveralRepositories', () => {
  it('is true only when the cards name more than one distinct repository', () => {
    const one = { repositories: [{ repo: 'github.com/acme/api', joined: 'default' as const }] };
    const other = { repositories: [{ repo: 'github.com/acme/docs', joined: 'filing' as const }] };
    expect(usesSeveralRepositories([one, one])).toBe(false);
    expect(usesSeveralRepositories([one, other])).toBe(true);
    expect(usesSeveralRepositories([{ repositories: [] }])).toBe(false);
  });

  it('drops the host from the name', () => {
    expect(repoName('github.com/acme/api')).toBe('acme/api');
  });
});

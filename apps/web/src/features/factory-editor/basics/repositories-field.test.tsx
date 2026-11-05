// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { BasicsSection } from '../../../shared/definition/model';
import { TooltipProvider } from '../../../ui/tooltip';
import { RepositoriesField } from './repositories-field';

const edit = vi.hoisted(() => vi.fn());
vi.mock('../draft/use-draft', () => ({
  useDraftSession: () => ({ edit }),
  useViewOnly: () => false,
}));
vi.mock('../frame/use-editor', () => ({ useProblemText: () => undefined }));
vi.mock('../frame/section-shell', () => ({ EditedMark: () => null }));
vi.mock('../frame/problem-fixes', () => ({ ProblemFixes: () => null }));
vi.mock('../environments/new-secret', () => ({ NewSecretSheetFor: () => ({ open: vi.fn(), sheet: null }) }));
vi.mock('../environments/parts', () => ({
  SecretSelect: ({ label }: { label: string }) => <button type="button">{label}</button>,
}));

afterEach(() => {
  cleanup();
  edit.mockClear();
});

const shop = { name: 'shop', url: 'https://github.com/acme/shop', access: 'write', default: true } as const;

/** The basics the last edit sets. */
const lastBasics = () => edit.mock.lastCall?.[0].value as BasicsSection;

describe('RepositoriesField', () => {
  it('says there is no code for a factory with none', () => {
    render(
      <TooltipProvider>
        <RepositoriesField basics={{}} shorthand={false} />
      </TooltipProvider>,
    );
    expect(screen.getByText('No code. Agents work through their tools.')).toBeTruthy();
  });

  it('adds a read only entry with a fresh name', () => {
    render(
      <TooltipProvider>
        <RepositoriesField basics={{ repositories: [shop] }} shorthand={false} />
      </TooltipProvider>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Add a repository' }));
    expect(lastBasics().repositories).toEqual([shop, { name: 'repo', url: '', access: 'read' }]);
  });

  it('changes access and moves the default', () => {
    const docs = { name: 'docs', url: 'https://github.com/acme/docs', access: 'read' } as const;
    render(
      <TooltipProvider>
        <RepositoriesField basics={{ repositories: [shop, docs] }} shorthand={false} />
      </TooltipProvider>,
    );
    fireEvent.click(screen.getByRole('radio', { name: 'Read only', checked: false }));
    expect(lastBasics().repositories?.[0]).toEqual({ ...shop, access: 'read' });
    fireEvent.click(screen.getAllByRole('checkbox', { name: 'Default' })[1] as HTMLElement);
    expect(lastBasics().repositories).toEqual([
      { name: 'shop', url: shop.url, access: 'write' },
      { ...docs, default: true },
    ]);
  });

  it('removes an entry, and the key when none is left', () => {
    render(
      <TooltipProvider>
        <RepositoriesField basics={{ repositories: [shop] }} shorthand={false} />
      </TooltipProvider>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Remove repository shop' }));
    expect(lastBasics()).not.toHaveProperty('repositories');
  });

  it('keeps an owner’s every repository from being the default', () => {
    const owner = { name: 'acme', url: 'https://github.com/acme/*', access: 'read' } as const;
    render(
      <TooltipProvider>
        <RepositoriesField basics={{ repositories: [shop, owner] }} shorthand={false} />
      </TooltipProvider>,
    );
    expect((screen.getAllByRole('checkbox', { name: 'Default' })[1] as HTMLInputElement).disabled).toBe(true);
  });
});

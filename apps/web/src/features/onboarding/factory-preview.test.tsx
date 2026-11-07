// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { codeCard } from './factory-preview';

afterEach(cleanup);

const marks = (container: HTMLElement) => container.querySelectorAll('svg');

describe('codeCard', () => {
  it('is empty before the Code step is done and there is no repository', () => {
    expect(codeCard({})).toBeUndefined();
  });

  it('says No code once the step is done, with no placeholder bars', () => {
    const { container } = render(<>{codeCard({ done: true })}</>);
    expect(screen.getByText('No code')).toBeTruthy();
    expect(screen.getByText("Items don't touch a repository")).toBeTruthy();
    expect(container.querySelector('.border-dashed')).toBeNull();
    expect(container.querySelector('.rounded-full')).toBeNull();
  });

  it('draws the GitLab mark for gitlab.com, not the GitHub one', () => {
    const { container } = render(<>{codeCard({ url: 'https://gitlab.com/acme/app', done: true })}</>);
    expect(screen.getByText('acme/app')).toBeTruthy();
    const svg = marks(container);
    expect(svg).toHaveLength(1);
    expect(svg[0]?.getAttribute('fill')).toBe('currentColor');
    expect(svg[0]?.querySelector('path')?.getAttribute('d')).toMatch(/^M8 15\.2/);
  });

  it('draws the GitHub mark for github.com and a neutral mark for another host', () => {
    const gh = render(<>{codeCard({ url: 'https://github.com/acme/app' })}</>);
    const ghPath = marks(gh.container)[0]?.querySelector('path')?.getAttribute('d');
    expect(ghPath).toMatch(/^M8 0C3\.58/);
    cleanup();
    const gl = render(<>{codeCard({ url: 'https://gitlab.com/acme/app' })}</>);
    const glPath = marks(gl.container)[0]?.querySelector('path')?.getAttribute('d');
    expect(glPath).not.toBe(ghPath);
    cleanup();
    const other = render(<>{codeCard({ url: 'https://git.example.com/acme/app' })}</>);
    const svg = marks(other.container)[0];
    expect(svg?.getAttribute('aria-hidden')).toBe('true');
    expect(svg?.querySelector('path')?.getAttribute('d')).not.toBe(ghPath);
    expect(svg?.querySelector('path')?.getAttribute('d')).not.toBe(glPath);
  });
});

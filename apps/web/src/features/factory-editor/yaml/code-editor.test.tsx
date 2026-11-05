// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { createRef } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { type CodeEditorHandle, TextareaEditor } from './code-editor';

afterEach(cleanup);

const base = { language: 'yaml' as const, value: 'a: 1', onChange: () => {}, label: 'factory.yaml' };

describe('the textarea editor', () => {
  it('uses 13px mono when it is the fallback for Monaco', () => {
    render(<TextareaEditor {...base} />);
    const area = screen.getByRole('textbox', { name: 'factory.yaml' });
    expect(area.className).toContain('text-[13px]');
    expect(area.className).not.toContain('text-base');
  });

  it('keeps 16px on a phone, so iOS does not zoom into the field', () => {
    render(<TextareaEditor {...base} phone />);
    expect(screen.getByRole('textbox', { name: 'factory.yaml' }).className).toContain('text-base');
  });
});

describe('revealing a line in the textarea editor', () => {
  it('moves only the editor for an automatic reveal, and may move the page when asked for', () => {
    const scrollIntoView = vi.fn();
    Element.prototype.scrollIntoView = scrollIntoView;
    const ref = createRef<CodeEditorHandle>();
    render(<TextareaEditor {...base} ref={ref} value={'a: 1\nb: 2'} />);
    expect(ref.current?.reveal(2)).toBe(true);
    expect(scrollIntoView).not.toHaveBeenCalled();
    expect(ref.current?.reveal(2, true)).toBe(true);
    expect(scrollIntoView).toHaveBeenCalledTimes(1);
  });
});

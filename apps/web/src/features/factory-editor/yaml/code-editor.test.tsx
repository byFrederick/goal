// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { TextareaEditor } from './code-editor';

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

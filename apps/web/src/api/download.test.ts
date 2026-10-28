// @vitest-environment jsdom
// The CSV export helpers: the filename goald names, a 413 that becomes an ApiError the screen can show,
// and the saved file. The browser's download is stubbed; the test reads the link it would click.
import { ApiError, type Result } from '@goal/api-client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { csvFileOf, exportFailure, filenameOf, saveCsv } from './download';

afterEach(() => {
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe('filenameOf', () => {
  it('reads the filename goald names in Content-Disposition', () => {
    expect(filenameOf('attachment; filename="runs-2026-10-27.csv"', 'runs.csv')).toBe('runs-2026-10-27.csv');
  });

  it('falls back to the given name when the header is missing or has no filename', () => {
    expect(filenameOf(null, 'spend.csv')).toBe('spend.csv');
    expect(filenameOf('attachment', 'spend.csv')).toBe('spend.csv');
  });
});

describe('csvFileOf', () => {
  it('returns the file and the name goald gave it', async () => {
    const response = new Response('id,factory\n', {
      headers: { 'Content-Type': 'text/csv', 'Content-Disposition': 'attachment; filename="runs-2026-10-27.csv"' },
    });
    const file = csvFileOf({ data: await response.clone().blob(), response }, 'runs.csv');

    expect(file.filename).toBe('runs-2026-10-27.csv');
    expect(await file.blob.text()).toBe('id,factory\n');
  });

  it('throws the problem of a 413 export-too-large as an ApiError', () => {
    const body = { title: 'Export too large', status: 413, detail: 'More than 100000 runs match; narrow the filters.' };
    const response = new Response(JSON.stringify(body), {
      status: 413,
      headers: { 'Content-Type': 'application/problem+json' },
    });
    const result: Result<Blob> = { error: body, response };

    expect(() => csvFileOf(result, 'runs.csv')).toThrow(ApiError);
    try {
      csvFileOf(result, 'runs.csv');
    } catch (error) {
      expect(exportFailure(error, 'runs')).toBe('Export too large: More than 100000 runs match; narrow the filters.');
    }
  });
});

describe('exportFailure', () => {
  it('says what went wrong without a problem', () => {
    expect(exportFailure(new TypeError('Failed to fetch'), 'spend')).toBe(
      'Couldn’t export spend. Check your connection and try again.',
    );
  });
});

describe('saveCsv', () => {
  let createObjectURL: ReturnType<typeof vi.fn>;
  let revokeObjectURL: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    createObjectURL = vi.fn(() => 'blob:goal-test');
    revokeObjectURL = vi.fn();
    Object.defineProperty(URL, 'createObjectURL', { value: createObjectURL, configurable: true });
    Object.defineProperty(URL, 'revokeObjectURL', { value: revokeObjectURL, configurable: true });
  });

  it('clicks a link to the file under goald’s filename, then releases the object URL', () => {
    vi.useFakeTimers();
    const clicked: { href: string; download: string }[] = [];
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      clicked.push({ href: this.href, download: this.download });
    });

    saveCsv({ blob: new Blob(['id\n']), filename: 'runs-2026-10-27.csv' });

    expect(createObjectURL).toHaveBeenCalledOnce();
    expect(clicked).toEqual([{ href: 'blob:goal-test', download: 'runs-2026-10-27.csv' }]);
    expect(document.querySelector('a[download]')).toBeNull();
    vi.runAllTimers();
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:goal-test');
  });
});

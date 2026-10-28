import { describe, expect, it } from 'vitest';
import {
  readCollapsed,
  readSize,
  readWidth,
  type SidebarStorage,
  sidebarKey,
  sidebarWidthKey,
  writeWidth,
} from './sidebar-choice';

/** A storage holding the given values; `throws` makes every call refuse, as blocked site data does. */
function storage(values: Record<string, string>, throws = false): SidebarStorage {
  return {
    getItem: (key) => {
      if (throws) throw new DOMException('blocked', 'SecurityError');
      return values[key] ?? null;
    },
    setItem: (key, value) => {
      if (throws) throw new DOMException('blocked', 'SecurityError');
      values[key] = value;
    },
  };
}

describe('readWidth', () => {
  it('reads a saved width in range', () => {
    expect(readWidth(storage({ [sidebarWidthKey]: '320' }))).toBe(320);
    expect(readWidth(storage({ [sidebarWidthKey]: '216' }))).toBe(216);
    expect(readWidth(storage({ [sidebarWidthKey]: '400' }))).toBe(400);
  });

  it.each([
    ['a missing value', undefined],
    ['a non-numeric value', 'wide'],
    ['an empty value', ''],
    ['a value below the minimum', '215'],
    ['a value above the maximum', '401'],
    ['a fractional value', '300.5'],
    ['a value that is not finite', 'Infinity'],
  ])('reads %s as the default 256', (_, raw) => {
    const values = raw === undefined ? {} : { [sidebarWidthKey]: raw };
    expect(readWidth(storage(values))).toBe(256);
  });

  it('reads a refusing storage and a missing storage as the default', () => {
    expect(readWidth(storage({ [sidebarWidthKey]: '320' }, true))).toBe(256);
    expect(readWidth(undefined)).toBe(256);
  });
});

describe('writeWidth', () => {
  it('saves the width under its key, clamped to the range', () => {
    const values: Record<string, string> = {};
    writeWidth(storage(values), 500);
    expect(values[sidebarWidthKey]).toBe('400');
  });

  it('does not throw when storage refuses', () => {
    expect(() => writeWidth(storage({}, true), 300)).not.toThrow();
  });
});

describe('the saved choice', () => {
  it('keeps reading the collapsed and expanded strings as before', () => {
    expect(readCollapsed(storage({ [sidebarKey]: 'collapsed' }))).toBe(true);
    expect(readCollapsed(storage({ [sidebarKey]: 'expanded' }))).toBe(false);
    expect(readCollapsed(storage({}))).toBe(false);
  });

  it('reads the collapsed choice and the width together, without a saved width', () => {
    expect(readSize(storage({ [sidebarKey]: 'collapsed' }))).toEqual({ collapsed: true, width: 256 });
  });

  it('reads the collapsed choice as expanded when storage refuses', () => {
    expect(readSize(storage({ [sidebarKey]: 'collapsed' }, true))).toEqual({ collapsed: false, width: 256 });
  });
});

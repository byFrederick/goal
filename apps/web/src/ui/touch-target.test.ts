import { describe, expect, it } from 'vitest';
import { touchLink, touchLinkWide, touchTarget } from './touch-target';

const words = (classes: string) => classes.split(' ');

describe('touchTarget', () => {
  it('grows the hit area by 8 px above and below, below 768 px only', () => {
    expect(words(touchTarget)).toEqual(
      expect.arrayContaining([
        'relative',
        'max-md:before:absolute',
        'max-md:before:-inset-y-2',
        'max-md:before:inset-x-0',
      ]),
    );
  });

  it('adds no motion and no raw values', () => {
    expect(touchTarget).not.toMatch(/transition|animate|duration|cubic-bezier|\d+px/);
  });
});

describe('touchLink', () => {
  it('pads a text link to at least 44 px below 768 px and takes the padding back with a margin', () => {
    expect(words(touchLink)).toEqual(expect.arrayContaining(['max-md:py-4', 'max-md:-my-4']));
  });

  it('adds no motion', () => {
    expect(touchLink).not.toMatch(/transition|animate|duration/);
  });
});

describe('touchLinkWide', () => {
  it('pads a short link on both axes below 768 px and takes the padding back with margins', () => {
    expect(words(touchLinkWide)).toEqual(
      expect.arrayContaining(['max-md:px-3', 'max-md:-mx-3', 'max-md:py-4', 'max-md:-my-4']),
    );
  });

  it('adds no motion', () => {
    expect(touchLinkWide).not.toMatch(/transition|animate|duration/);
  });
});

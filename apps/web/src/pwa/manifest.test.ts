import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

// The install metadata of the home-screen app (docs/engineering/frontend.md, PWA): goald serves
// apps/web/public as it is, so these files are what a phone reads when Goal is added to its home screen.
const publicDir = fileURLToPath(new URL('../../public/', import.meta.url));
const indexHtml = readFileSync(fileURLToPath(new URL('../../index.html', import.meta.url)), 'utf8');

interface Icon {
  src: string;
  sizes: string;
  type: string;
  purpose?: string;
}

interface Manifest {
  name: string;
  short_name: string;
  id: string;
  start_url: string;
  scope: string;
  display: string;
  background_color: string;
  theme_color: string;
  icons: Icon[];
}

const manifest = JSON.parse(readFileSync(`${publicDir}manifest.webmanifest`, 'utf8')) as Manifest;

// The width and height of a PNG are the big-endian uint32s at bytes 16 and 20 of its IHDR chunk.
function pngSize(file: string): { width: number; height: number } {
  const bytes = readFileSync(`${publicDir}${file}`);
  expect(bytes.subarray(1, 4).toString('ascii'), `${file} is a PNG`).toBe('PNG');

  return { width: bytes.readUInt32BE(16), height: bytes.readUInt32BE(20) };
}

// The metas the iOS home screen and status bar read, by name.
function meta(name: string): string | undefined {
  const tag = indexHtml.match(new RegExp(`<meta\\s+name="${name}"\\s+content="([^"]*)"`));

  return tag?.[1];
}

describe('web app manifest', () => {
  it('names the app Goal and opens it standalone at the root', () => {
    expect(manifest.name).toBe('Goal');
    expect(manifest.short_name).toBe('Goal');
    expect(manifest.id).toBe('/');
    expect(manifest.start_url).toBe('/');
    expect(manifest.scope).toBe('/');
    expect(manifest.display).toBe('standalone');
  });

  it('uses the light shell colour for both the background and the theme', () => {
    expect(manifest.background_color).toBe('#eef1ef');
    expect(manifest.theme_color).toBe('#eef1ef');
  });

  it.each([
    ['icons/icon-192.png', '192x192', 'any'],
    ['icons/icon-512.png', '512x512', 'any'],
    ['icons/icon-maskable-512.png', '512x512', 'maskable'],
  ])('lists %s at %s with purpose %s, and the file has that size', (src, sizes, purpose) => {
    const icon = manifest.icons.find((i) => i.src === `/${src}`);

    expect(icon).toMatchObject({ sizes, type: 'image/png', purpose });

    const [width, height] = sizes.split('x').map(Number);
    expect(pngSize(src)).toEqual({ width, height });
  });

  it('has a maskable icon', () => {
    expect(manifest.icons.some((i) => i.purpose?.split(' ').includes('maskable'))).toBe(true);
  });
});

describe('index.html', () => {
  it('links the manifest and the apple touch icon', () => {
    expect(indexHtml).toContain('<link rel="manifest" href="/manifest.webmanifest" />');
    expect(indexHtml).toContain('<link rel="apple-touch-icon" href="/apple-touch-icon.png" />');
  });

  it('carries the iOS metas for a standalone app with a translucent status bar', () => {
    expect(meta('apple-mobile-web-app-capable')).toBe('yes');
    expect(meta('mobile-web-app-capable')).toBe('yes');
    expect(meta('apple-mobile-web-app-status-bar-style')).toBe('black-translucent');
    expect(meta('apple-mobile-web-app-title')).toBe('Goal');
  });

  it('keeps viewport-fit=cover and the light and dark theme colours', () => {
    expect(indexHtml).toContain('viewport-fit=cover');
    expect(indexHtml).toContain('<meta name="theme-color" content="#eef1ef" media="(prefers-color-scheme: light)" />');
    expect(indexHtml).toContain('<meta name="theme-color" content="#0f161d" media="(prefers-color-scheme: dark)" />');
  });
});

describe('apple touch icon', () => {
  it('is 180x180', () => {
    expect(pngSize('apple-touch-icon.png')).toEqual({ width: 180, height: 180 });
  });

  it('is opaque: an RGB PNG, since iOS shows transparent pixels as black', () => {
    const bytes = readFileSync(`${publicDir}apple-touch-icon.png`);
    // IHDR bit depth at byte 24, colour type at byte 25: 8 and 2 (RGB), not 6 (RGBA).
    expect(bytes[24]).toBe(8);
    expect(bytes[25]).toBe(2);
  });
});

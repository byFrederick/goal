// Import YAML's upload (D §10.11): a factory.yaml, or a definition folder (factory.yaml, agents/,
// skills/, contracts/), becomes B's FileMap. A folder's own name is dropped so its files sit at
// the definition's root; hidden files are left out (a definition may not hold them); text stays
// text and anything that is not UTF-8 travels as base64. Limits: 500 files, 5 MiB.
import type { FileMap } from '../../../api/types';

export const uploadLimits = { files: 500, bytes: 5 * 1024 * 1024 } as const;

export interface Uploaded {
  /** The path the browser gave: the file's name, or its path under the chosen folder. */
  path: string;
  bytes: Uint8Array;
}

export type UploadResult = { files: FileMap } | { error: string };

const decoder = new TextDecoder('utf-8', { fatal: true });

function base64(bytes: Uint8Array): string {
  let s = '';
  for (let i = 0; i < bytes.length; i += 0x8000) s += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(s);
}

/** The files as a FileMap rooted at the definition, or why they can't be imported. */
export function filesFromUpload(uploaded: readonly Uploaded[]): UploadResult {
  const parts = uploaded.map((u) => u.path.split('/').filter(Boolean));
  // A folder upload names every file under the folder: drop that first segment.
  const folder = parts.length > 0 && parts.every((p) => p.length > 1 && p[0] === parts[0]?.[0]);
  const files: FileMap = {};
  let count = 0;
  let size = 0;
  uploaded.forEach((u, i) => {
    const segments = (parts[i] ?? []).slice(folder ? 1 : 0);
    if (segments.length === 0 || segments.some((s) => s.startsWith('.'))) return;
    count++;
    size += u.bytes.byteLength;
    let text: string | undefined;
    try {
      text = decoder.decode(u.bytes);
    } catch {}
    // One file on its own is the factory.yaml, whatever it is called.
    const path = uploaded.length === 1 && !folder ? 'factory.yaml' : segments.join('/');
    files[path] = text !== undefined && !text.includes('\u0000') ? { text } : { base64: base64(u.bytes) };
  });
  if (count > uploadLimits.files) return { error: `That is ${count} files; a definition holds at most 500.` };
  if (size > uploadLimits.bytes) return { error: 'That is more than 5 MiB; a definition holds at most 5 MiB.' };
  if (!('factory.yaml' in files)) return { error: 'There is no factory.yaml at the top of what you chose.' };
  return { files };
}

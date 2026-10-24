// JSON values and RFC 6901 pointers, as the draft's merge, fixes and session use them: equality
// whatever the key order, and immutable reads and writes at a pointer.

export type Json = Record<string, unknown>;

export const isObject = (v: unknown): v is Json => typeof v === 'object' && v !== null && !Array.isArray(v);

/** A JSON text of v with object keys sorted and undefined members left out. */
export function canonical(v: unknown): string {
  if (Array.isArray(v)) return `[${v.map(canonical).join(',')}]`;
  if (isObject(v)) {
    const keys = Object.keys(v)
      .filter((k) => v[k] !== undefined)
      .sort();
    return `{${keys.map((k) => `${JSON.stringify(k)}:${canonical(v[k])}`).join(',')}}`;
  }
  return v === undefined ? 'undefined' : JSON.stringify(v);
}

/** Deep equality of JSON values, whatever the order of object keys. */
export const equal = (a: unknown, b: unknown) => a === b || canonical(a) === canonical(b);

export const escapeToken = (k: string) => k.replaceAll('~', '~0').replaceAll('/', '~1');

/** The tokens of a pointer ("" is the whole value). */
export function pointerTokens(pointer: string): string[] {
  return pointer === ''
    ? []
    : pointer
        .slice(1)
        .split('/')
        .map((t) => t.replaceAll('~1', '/').replaceAll('~0', '~'));
}

/** Orders pointers the way their values sit: list indices numerically. */
export function comparePointers(a: string, b: string): number {
  const x = pointerTokens(a);
  const y = pointerTokens(b);
  for (let i = 0; i < Math.min(x.length, y.length); i++) {
    const s = x[i] as string;
    const t = y[i] as string;
    if (s === t) continue;
    const m = Number(s);
    const n = Number(t);
    if (Number.isInteger(m) && Number.isInteger(n)) return m - n;
    return s < t ? -1 : 1;
  }
  return x.length - y.length;
}

/** The value at a pointer; undefined when any part is missing. */
export function getPointer(value: unknown, pointer: string): unknown {
  let v = value;
  for (const tok of pointerTokens(pointer)) {
    if (Array.isArray(v)) v = v[Number(tok)];
    else if (isObject(v)) v = v[tok];
    else return undefined;
  }
  return v;
}

/**
 * A copy of value with `next` at pointer (removed when next is undefined; `-` appends to a list).
 * Only the path to the pointer is copied; a path whose parent is missing is created as objects.
 */
export function setPointer<T>(value: T, pointer: string, next: unknown): T {
  return setAt(value, pointerTokens(pointer), next) as T;
}

function setAt(v: unknown, toks: string[], next: unknown): unknown {
  if (toks.length === 0) return next;
  const [tok, ...rest] = toks as [string, ...string[]];
  if (Array.isArray(v)) {
    const i = tok === '-' ? v.length : Number(tok);
    if (!Number.isInteger(i) || i < 0 || i > v.length) return v;
    const out = [...v];
    if (rest.length === 0 && next === undefined) out.splice(i, 1);
    else out[i] = setAt(v[i], rest, next);
    return out;
  }
  const obj = isObject(v) ? v : {};
  if (rest.length === 0 && next === undefined) {
    const { [tok]: _gone, ...others } = obj;
    return others;
  }
  return { ...obj, [tok]: setAt(obj[tok], rest, next) };
}

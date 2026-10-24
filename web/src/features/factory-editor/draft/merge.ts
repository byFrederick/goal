// The three-way merge of a section after a 412 (D §10.4): base is the value our edits started
// from (the session's last server value), theirs the section as goald has it now, ours the local
// value. Changes to different leaves merge; a leaf both sides changed differently is a conflict
// (the dialog's "Build's WIP limit also changed elsewhere: theirs 4, yours 2"). Lists whose items
// all carry an `id` (stations) or a `name` (agents, skills, connections) are matched by it, never
// by index, so a reorder on one side and an edit on the other merge; other lists are one value.
import { comparePointers, equal, escapeToken, isObject, type Json, setPointer } from './pointer';

export interface Conflict {
  /** Where it is in the merged value: what `resolve` sets. */
  pointer: string;
  /** The same place by identity, for its label: ['stations', 'build', 'wip']. */
  path: string[];
  base: unknown;
  /** undefined: that side removed it. */
  theirs: unknown;
  ours: unknown;
}

export interface Merged<T> {
  /** The merged value; at each conflict it holds one side for now (ours, or the item a side removed). */
  value: T;
  conflicts: Conflict[];
}

/** Merges ours and theirs, both changed from base. */
export function merge3<T>(base: T, theirs: T, ours: T): Merged<T> {
  const conflicts: Conflict[] = [];
  const value = mergeValue(base, theirs, ours, [], '', conflicts) as T;
  return { value, conflicts };
}

/** The merged value with each conflict decided: theirs where `keep` says so, ours everywhere else. */
export function resolve<T>(merged: Merged<T>, keep: Readonly<Record<string, 'theirs' | 'ours'>>): T {
  let v = merged.value;
  const later = [...merged.conflicts].sort((a, b) => comparePointers(b.pointer, a.pointer));
  for (const c of later) {
    v = setPointer(v, c.pointer, structuredClone(keep[c.pointer] === 'theirs' ? c.theirs : c.ours));
  }
  return v;
}

function mergeValue(b: unknown, t: unknown, o: unknown, path: string[], pointer: string, out: Conflict[]): unknown {
  if (equal(t, o) || equal(b, o)) return t;
  if (equal(b, t)) return o;
  if (isObject(t) && isObject(o) && (isObject(b) || b === undefined))
    return mergeObject(b ?? {}, t, o, path, pointer, out);
  if (Array.isArray(t) && Array.isArray(o) && (Array.isArray(b) || b === undefined)) {
    const key = identityKey([...(b ?? []), ...t, ...o]);
    if (key) return mergeList(b ?? [], t, o, key, path, pointer, out);
  }
  out.push({ pointer, path, base: b, theirs: t, ours: o });
  return o;
}

function mergeObject(b: Json, t: Json, o: Json, path: string[], pointer: string, out: Conflict[]): Json {
  const result: Json = {};
  for (const k of new Set([...Object.keys(o), ...Object.keys(t), ...Object.keys(b)])) {
    const v = mergeValue(b[k], t[k], o[k], [...path, k], `${pointer}/${escapeToken(k)}`, out);
    if (v !== undefined) result[k] = v;
  }
  return result;
}

/** The key that names every item of the lists: id, else name; undefined when items lack one. */
function identityKey(items: unknown[]): 'id' | 'name' | undefined {
  if (items.length === 0) return undefined;
  for (const key of ['id', 'name'] as const) {
    if (items.every((it) => isObject(it) && typeof it[key] === 'string' && it[key] !== '')) return key;
  }
  return undefined;
}

function mergeList(
  b: unknown[],
  t: unknown[],
  o: unknown[],
  key: 'id' | 'name',
  path: string[],
  pointer: string,
  out: Conflict[],
) {
  const ids = (l: unknown[]) => l.map((it) => String((it as Json)[key]));
  const byId = (l: unknown[]) => new Map(l.map((it) => [String((it as Json)[key]), it]));
  const [B, T, O] = [byId(b), byId(t), byId(o)];

  // Which items stay, and whether a side's removal of a changed item asks first.
  const keep = new Map<string, { removedBy?: 'theirs' | 'ours' }>();
  for (const id of new Set([...ids(o), ...ids(t), ...ids(b)])) {
    const [inB, inT, inO] = [B.has(id), T.has(id), O.has(id)];
    if (inT && inO) keep.set(id, {});
    else if (!inB)
      keep.set(id, {}); // added by one side
    else if (inO && !equal(B.get(id), O.get(id))) keep.set(id, { removedBy: 'theirs' });
    else if (inT && !equal(B.get(id), T.get(id))) keep.set(id, { removedBy: 'ours' });
  }

  // Theirs' order, unless we reordered; then items only the other side has, after their predecessor.
  const reordered = (side: string[]) => {
    const common = ids(b).filter((id) => side.includes(id));
    return !equal(
      common,
      side.filter((id) => common.includes(id)),
    );
  };
  const [first, other] = reordered(ids(o)) ? [ids(o), ids(t)] : [ids(t), ids(o)];
  const order = first.filter((id) => keep.has(id));
  other.forEach((id, i) => {
    if (!keep.has(id) || order.includes(id)) return;
    let at = 0;
    for (let j = i - 1; j >= 0; j--) {
      const k = order.indexOf(other[j] as string);
      if (k >= 0) {
        at = k + 1;
        break;
      }
    }
    order.splice(at, 0, id);
  });
  for (const id of keep.keys()) if (!order.includes(id)) order.push(id); // kept by base only

  return order.map((id, i) => {
    const at = `${pointer}/${i}`;
    const removedBy = keep.get(id)?.removedBy;
    if (removedBy) {
      const item = removedBy === 'theirs' ? O.get(id) : T.get(id);
      out.push({
        pointer: at,
        path: [...path, id],
        base: B.get(id),
        theirs: removedBy === 'theirs' ? undefined : item,
        ours: removedBy === 'ours' ? undefined : item,
      });
      return item;
    }
    return mergeValue(B.get(id), T.get(id), O.get(id), [...path, id], at, out);
  });
}

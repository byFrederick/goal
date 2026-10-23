import { useLoaderData } from '@tanstack/react-router';
import type { Factory, StationKind } from '../api/types';
import { humanize } from './format';

/** The factory of the current /f/$factory route. */
export function useFactory(): Factory {
  return useLoaderData({ from: '/f/$factory' }).factory;
}

export function factoryTitle(f: { name: string; title?: string | undefined }): string {
  return f.title || humanize(f.name);
}

export function stationKind(factory: Factory, station: string): StationKind | undefined {
  return factory.stations.find((s) => s.id === station)?.kind;
}

/** The API key of an item route's $ref: a bare number becomes `<factory>#<number>`. */
export function itemKey(factory: string, ref: string): string {
  return /^\d+$/.test(ref) ? `${factory}#${ref}` : ref;
}

/** The route param for an item ref: "goal#12" → "12". */
export function refParam(ref: string): string {
  const hash = ref.lastIndexOf('#');
  return hash >= 0 ? ref.slice(hash + 1) : ref;
}

const lastFactoryKey = 'goal.factory';

export function rememberFactory(name: string): void {
  try {
    localStorage.setItem(lastFactoryKey, name);
  } catch {
    // Storage can be unavailable (private windows); the floor then opens the first factory.
  }
}

/** The factory the floor opens: the one used last, else the first goald lists. */
export function defaultFactory(factories: Factory[]): Factory | undefined {
  let last: string | null = null;
  try {
    last = localStorage.getItem(lastFactoryKey);
  } catch {
    last = null;
  }
  return factories.find((f) => f.name === last) ?? factories[0];
}

import { useRouteContext } from '@tanstack/react-router';
import type { Factory, StationKind } from '../api/types';
import { humanize } from './format';

/** The factory of the current /o/$org/f/$factory route; its queries are keyed by its `id`. */
export function useFactory(): Factory {
  return useRouteContext({ from: '/o/$org/f/$factory' }).factory;
}

/** The canonical params of the current factory's pages (organization slug, factory name), for links. */
export function useFactoryParams(): { org: string; factory: string } {
  const { org, factory } = useRouteContext({ from: '/o/$org/f/$factory' });
  return { org: org.slug, factory: factory.name };
}

/** A factory of the organization by the URL's $factory: its name or its id. */
export function findFactory(factories: readonly Factory[], key: string): Factory | undefined {
  return factories.find((f) => f.name === key || f.id === key);
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

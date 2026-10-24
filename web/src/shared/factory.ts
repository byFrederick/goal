import { useQuery } from '@tanstack/react-query';
import { useRouteContext } from '@tanstack/react-router';
import { useCallback } from 'react';
import { factoriesQuery } from '../api/queries';
import type { Factory } from '../api/types';
import { humanize } from './format';

/**
 * The factory of the current /o/$org/f/$factory route, as the organization's factory list has it
 * now: a publish elsewhere (its `definition` event refetches the list) changes its stations, work
 * types and version on the open page. Its queries are keyed by its `id`, which never changes; the
 * layout's copy, resolved when the route loaded, stands in until the list has it.
 */
export function useFactory(): Factory {
  const { org, factory } = useRouteContext({ from: '/o/$org/f/$factory' });
  const select = useCallback((list: Factory[]) => list.find((f) => f.id === factory.id), [factory.id]);
  return useQuery({ ...factoriesQuery(org.id), select }).data ?? factory;
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

/** The API key of an item route's $ref: a bare number becomes `<factory>#<number>`. */
export function itemKey(factory: string, ref: string): string {
  return /^\d+$/.test(ref) ? `${factory}#${ref}` : ref;
}

/** The route param for an item ref: "goal#12" → "12". */
export function refParam(ref: string): string {
  const hash = ref.lastIndexOf('#');
  return hash >= 0 ? ref.slice(hash + 1) : ref;
}

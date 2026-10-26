import { createFileRoute } from '@tanstack/react-router';
import { factoriesQuery, knowledgeSourcesQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { KnowledgePage } from '../features/knowledge/knowledge-page';
import { type KnowledgeSearch, knowledgeSearch } from '../features/knowledge/search';

// The organization's knowledge base (spec 4 §10, canvas "Knowledge" and "Mobile-Knowledge"):
// ?factory= (a factory's name: the sources its current version lists; the factory sidebar's
// Knowledge), ?source= (whose documents show beside the list), ?q= and ?as= (the search tried, as
// a factory). The lists load with the page, not before it: when they fail the page keeps its
// header and New source.
export const Route = createFileRoute('/o/$org/_org/knowledge/')({
  validateSearch: (search: Record<string, unknown>): KnowledgeSearch => knowledgeSearch(search),
  loaderDeps: ({ search }) => ({ factory: search.factory }),
  loader: ({ context: { queryClient, org }, deps }) => {
    const factory = deps.factory
      ? queryClient.getQueryData(factoriesQuery(org.id).queryKey)?.find((f) => f.name === deps.factory)
      : undefined;
    void queryClient.prefetchQuery(knowledgeSourcesQuery(org.id));
    if (factory) void queryClient.prefetchQuery(knowledgeSourcesQuery(org.id, { factory: factory.id }));
  },
  head: ({ match: { context } }) => pageTitle('Knowledge', context.org.name),
  component: KnowledgePage,
});

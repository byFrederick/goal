import { createFileRoute } from '@tanstack/react-router';
import { factoriesQuery, knowledgeReadsQuery, knowledgeSourcesQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { ListPageSkeleton } from '../app/route-skeletons';
import { KnowledgePage } from '../features/knowledge/knowledge-page';
import { type KnowledgeSearch, knowledgeSearch } from '../features/knowledge/search';

// The organization's knowledge sources (ADR 0038 decision 12, canvas "Knowledge" and "Mobile-Knowledge"):
// ?factory= (a factory's name: the sources its current version lists; the factory sidebar's Knowledge)
// and ?q= (Find a file). The lists load with the page, behind its skeleton, so a failure keeps the header.
export const Route = createFileRoute('/o/$org/_org/knowledge/')({
  pendingComponent: ListPageSkeleton,
  validateSearch: (search: Record<string, unknown>): KnowledgeSearch => knowledgeSearch(search),
  loaderDeps: ({ search }) => ({ factory: search.factory }),
  loader: ({ context: { queryClient, org }, deps }) => {
    const factory = deps.factory
      ? queryClient.getQueryData(factoriesQuery(org.id).queryKey)?.find((f) => f.name === deps.factory)
      : undefined;
    void queryClient.prefetchQuery(knowledgeSourcesQuery(org.id));
    void queryClient.prefetchQuery(knowledgeReadsQuery(org.id));
    if (factory) void queryClient.prefetchQuery(knowledgeSourcesQuery(org.id, { factory: factory.id }));
  },
  head: ({ match: { context } }) => pageTitle('Knowledge', context.org.name),
  component: KnowledgePage,
});

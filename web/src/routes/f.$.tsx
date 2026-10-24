import { createFileRoute, notFound, redirect } from '@tanstack/react-router';
import { factoriesQuery, orgsQuery } from '../api/queries';
import { NotFoundScreen } from '../app/states';
import { findFactory } from '../shared/factory';
import { legacyPath, legacyTarget } from '../shared/legacy';
import { lastOrgSlug } from '../shared/org';

// Links from before organizations (/f/:factory/...): finds the factory in the organization used
// last, then in every other, and moves to its page under /o/:org, keeping the search params.
export const Route = createFileRoute('/f/$')({
  loader: async ({ context: { queryClient }, params, location }) => {
    const target = legacyTarget(params._splat ?? '');
    if (!target) throw notFound();

    const last = lastOrgSlug();
    const orgs = (await queryClient.ensureQueryData(orgsQuery)).toSorted(
      (a, b) => Number(b.slug === last) - Number(a.slug === last),
    );
    for (const org of orgs) {
      const factories = await queryClient.ensureQueryData(factoriesQuery(org.id));
      if (findFactory(factories, target.factory)) {
        throw redirect({ href: `${legacyPath(org.slug, target)}${location.searchStr}`, replace: true });
      }
    }
    throw notFound();
  },
  notFoundComponent: () => (
    <main id="main" className="p-6">
      <NotFoundScreen what="factory" />
    </main>
  ),
});

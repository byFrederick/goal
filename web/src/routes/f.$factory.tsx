import { createFileRoute, notFound, Outlet } from '@tanstack/react-router';
import { useEffect } from 'react';
import { factoriesQuery } from '../api/queries';
import { Shell } from '../app/shell';
import { NotFoundScreen } from '../app/states';
import { rememberFactory, useFactory } from '../shared/factory';
import { interimOrg, useOrg } from '../shared/org';
import { FactoryStream } from '../shared/stream';

// Every factory screen: the shell, and one live stream for the factory.
export const Route = createFileRoute('/f/$factory')({
  // Resolved before the children load, so they key their queries by the organization's id and
  // the factory's name even when the URL names the factory by id. The organization is the
  // interim first one (shared/org.ts) until C's /o/:org routes name it.
  beforeLoad: async ({ context, params }) => {
    const org = await interimOrg(context.queryClient);
    if (!org) throw notFound();
    const factories = await context.queryClient.ensureQueryData(factoriesQuery(org.id));
    const factory = factories.find((f) => f.name === params.factory || f.id === params.factory);
    if (!factory) throw notFound();
    return { org, factory };
  },
  loader: ({ context }) => ({ org: context.org, factory: context.factory }),
  component: FactoryLayout,
  notFoundComponent: () => (
    <main className="p-6">
      <NotFoundScreen what="factory" />
    </main>
  ),
});

function FactoryLayout() {
  const org = useOrg();
  const factory = useFactory();

  useEffect(() => rememberFactory(factory.name), [factory.name]);

  return (
    <FactoryStream org={org} factory={factory.name}>
      <Shell factory={factory}>
        <Outlet />
      </Shell>
    </FactoryStream>
  );
}

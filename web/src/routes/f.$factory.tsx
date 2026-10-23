import { createFileRoute, notFound, Outlet } from '@tanstack/react-router';
import { useEffect } from 'react';
import { factoriesQuery } from '../api/queries';
import { Shell } from '../app/shell';
import { NotFoundScreen } from '../app/states';
import { rememberFactory, useFactory } from '../shared/factory';
import { FactoryStream } from '../shared/stream';

// Every factory screen: the shell, and one live stream for the factory.
export const Route = createFileRoute('/f/$factory')({
  // Resolved before the children load, so they key their queries by the factory's name even when
  // the URL names it by id.
  beforeLoad: async ({ context, params }) => {
    const factories = await context.queryClient.ensureQueryData(factoriesQuery);
    const factory = factories.find((f) => f.name === params.factory || f.id === params.factory);
    if (!factory) throw notFound();
    return { factory };
  },
  loader: ({ context }) => ({ factory: context.factory }),
  component: FactoryLayout,
  notFoundComponent: () => (
    <main className="p-6">
      <NotFoundScreen what="factory" />
    </main>
  ),
});

function FactoryLayout() {
  const factory = useFactory();

  useEffect(() => rememberFactory(factory.name), [factory.name]);

  return (
    <FactoryStream factory={factory.name}>
      <Shell factory={factory}>
        <Outlet />
      </Shell>
    </FactoryStream>
  );
}

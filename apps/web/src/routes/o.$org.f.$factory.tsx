import { createFileRoute, notFound, Outlet } from '@tanstack/react-router';
import { useEffect } from 'react';
import { archivedFactoriesQuery, ensureFound, factoriesQuery } from '../api/queries';
import { ShellFrame } from '../app/app-shell';
import { ArchivedBanner } from '../app/archived-banner';
import { NotFoundScreen } from '../app/states';
import { findFactory, useFactory } from '../shared/factory';
import { rememberFactory } from '../shared/org';

// Every page of one factory. The sidebar and, on phones, the top bar and tab bar are AppShell's and
// ShellFrame's. An archived factory is not in the list, so it is looked up in the archived list: its pages stay
// readable, under a banner (ArchivedBanner).
export const Route = createFileRoute('/o/$org/f/$factory')({
  // Resolved before the children load, so they key their queries by the factory's id even when
  // the URL names it by name (or the other way round).
  beforeLoad: async ({ context, params }) => {
    const factory =
      (await ensureFound(context.queryClient, factoriesQuery(context.org.id), (factories) =>
        findFactory(factories, params.factory),
      )) ??
      (await ensureFound(context.queryClient, archivedFactoriesQuery(context.org.id), (factories) =>
        findFactory(factories, params.factory),
      ));
    if (!factory) throw notFound();
    return { factory };
  },
  component: FactoryLayout,
  notFoundComponent: FactoryNotFound,
});

function FactoryLayout() {
  const { org } = Route.useRouteContext();
  const factory = useFactory();
  // The phone's tab bar opens this factory from the organization's other pages.
  useEffect(() => rememberFactory(org.id, factory.id), [org.id, factory.id]);
  return (
    <ShellFrame>
      <ArchivedBanner />
      <Outlet />
    </ShellFrame>
  );
}

function FactoryNotFound() {
  return (
    <ShellFrame>
      <NotFoundScreen what="factory" />
    </ShellFrame>
  );
}

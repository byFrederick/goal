import { createFileRoute, notFound, Outlet } from '@tanstack/react-router';
import { archivedFactoriesQuery, ensureFound, factoriesQuery } from '../api/queries';
import { ShellFrame } from '../app/app-shell';
import { ArchivedBanner } from '../app/archived-banner';
import { PhoneCrumb, TabBar } from '../app/phone-chrome';
import { NotFoundScreen } from '../app/states';
import { findFactory, useFactory } from '../shared/factory';

// Every page of one factory: on phones the crumb and the tab bar (the sidebar is AppShell's). An
// archived factory is not in the list, so it is looked up in the archived list: its pages stay
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
  return (
    <ShellFrame crumb={<PhoneCrumb org={org} factory={factory} />} tabBar={<TabBar org={org} factory={factory} />}>
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

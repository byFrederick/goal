import { createFileRoute, notFound, Outlet } from '@tanstack/react-router';
import { ensureFound, factoriesQuery } from '../api/queries';
import { ShellFrame } from '../app/app-shell';
import { FactorySidebar } from '../app/factory-sidebar';
import { PhoneCrumb, TabBar } from '../app/phone-chrome';
import { NotFoundScreen } from '../app/states';
import { findFactory, useFactory } from '../shared/factory';

// Every page of one factory: the factory sidebar, and on phones the crumb and the tab bar.
export const Route = createFileRoute('/o/$org/f/$factory')({
  // Resolved before the children load, so they key their queries by the factory's id even when
  // the URL names it by name (or the other way round).
  beforeLoad: async ({ context, params }) => {
    const factory = await ensureFound(context.queryClient, factoriesQuery(context.org.id), (factories) =>
      findFactory(factories, params.factory),
    );
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
    <ShellFrame
      sidebar={<FactorySidebar org={org} factory={factory} />}
      crumb={<PhoneCrumb org={org} factory={factory} />}
      tabBar={<TabBar org={org} factory={factory} />}
    >
      <Outlet />
    </ShellFrame>
  );
}

function FactoryNotFound() {
  return (
    <ShellFrame sidebar={null}>
      <NotFoundScreen what="factory" />
    </ShellFrame>
  );
}

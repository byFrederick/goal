import { createFileRoute, redirect } from '@tanstack/react-router';
import { factoriesQuery } from '../api/queries';
import { defaultFactory } from '../shared/factory';
import { interimOrg } from '../shared/org';
import { LogoMark } from '../ui/logo';

// The floor of the factory used last (or the first goald lists) of the interim organization.
export const Route = createFileRoute('/')({
  loader: async ({ context }) => {
    const org = await interimOrg(context.queryClient);
    const factories = org ? await context.queryClient.ensureQueryData(factoriesQuery(org.id)) : [];
    const factory = defaultFactory(factories);
    if (factory) throw redirect({ to: '/f/$factory', params: { factory: factory.name }, replace: true });
  },
  component: NoFactories,
});

function NoFactories() {
  return (
    <main className="mx-auto flex min-h-dvh max-w-xl flex-col justify-center gap-4 p-6">
      <LogoMark />
      <h1 className="m-0 text-4xl font-normal tracking-display">No factories yet</h1>
      <p className="m-0 text-[15px] leading-relaxed text-muted">
        goald runs the factories in its bucket. Push one with <code>goal factory push &lt;dir&gt;</code> and this page
        shows its floor.
      </p>
    </main>
  );
}

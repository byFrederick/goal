// The organization's home: one card per factory (canvas "Factories", "Mobile-Factories"). C3
// ships the cards' mark, name, purpose and Open; C5 adds health, the stats grid, the header
// sentence, Waiting on you and the New factory card.
import { useSuspenseQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { factoriesQuery } from '../../api/queries';
import type { Factory } from '../../api/types';
import { factoryMark } from '../../shared/marks';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';

export function FactoriesPage() {
  const org = useOrg();
  const { data } = useSuspenseQuery(factoriesQuery(org.id));
  const factories = data.toSorted((a, b) => a.name.localeCompare(b.name));

  return (
    <>
      <header className="flex flex-col gap-2">
        <p className="m-0 text-[13px] text-muted md:hidden">{org.name}</p>
        <h1 className="m-0 text-[34px] leading-[1.05] font-normal tracking-display md:text-5xl">Factories</h1>
        <p className="m-0 hidden text-[15px] text-muted md:block">{org.name}’s factories.</p>
      </header>

      {factories.length > 0 ? (
        <section aria-labelledby="factories-list">
          <h2 id="factories-list" className="sr-only">
            {org.name}’s factories
          </h2>
          <ul className="m-0 grid list-none gap-3 p-0 @[640px]:grid-cols-2 @[640px]:gap-4 @[1000px]:grid-cols-3">
            {factories.map((f) => (
              <li key={f.id} className="flex">
                <FactoryCard org={org.slug} factory={f} mark={factoryMark(f, factories)} />
              </li>
            ))}
          </ul>
        </section>
      ) : (
        <section className="flex max-w-xl flex-col gap-2 rounded-card border border-line bg-card p-5">
          <h2 className="m-0 text-xl font-medium">No factories in {org.name} yet</h2>
          <p className="m-0 text-[15px] leading-relaxed text-muted">
            A factory runs from a definition in the bucket. Push one with <code>goal factory push &lt;dir&gt;</code> and
            it appears here.
          </p>
        </section>
      )}
    </>
  );
}

function FactoryCard({ org, factory, mark }: { org: string; factory: Factory; mark: string }) {
  const params = { org, factory: factory.name };
  return (
    <article
      aria-labelledby={`factory-${factory.id}`}
      className="flex min-w-0 flex-1 flex-col gap-4 rounded-card border border-line bg-card p-4 md:p-5"
    >
      <div className="flex items-center gap-3">
        <span
          aria-hidden="true"
          className="flex size-10 shrink-0 items-center justify-center rounded-control border border-rule bg-surface text-base font-medium"
        >
          {mark}
        </span>
        <h2
          id={`factory-${factory.id}`}
          className="m-0 min-w-0 text-[26px] leading-[1.1] font-normal tracking-display break-words"
        >
          <Link to="/o/$org/f/$factory" params={params} className="no-underline hover:text-run">
            {factory.name}
          </Link>
        </h2>
      </div>
      {factory.description ? <p className="m-0 text-[15px] leading-[1.45]">{factory.description}</p> : null}
      <div className="mt-auto flex flex-wrap gap-2">
        <Button asChild variant="primary">
          <Link to="/o/$org/f/$factory" params={params}>
            Open {factory.name}
          </Link>
        </Button>
      </div>
    </article>
  );
}

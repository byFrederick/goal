// Clone a factory (D §10.11): the organization's published factories as radio rows. B copies the
// running version's files; items, history and credentials stay behind.
import { useQuery } from '@tanstack/react-query';
import { Copy } from 'lucide-react';
import { useId } from 'react';
import { factoriesQuery } from '../../../api/queries';
import { plural } from '../../../shared/format';
import { useOrg } from '../../../shared/org';
import { EmptyState } from '../../../ui/empty-state';
import { RadioList } from '../../../ui/radio-cards';

export function ClonePicker({ value, onChange }: { value: string | undefined; onChange: (id: string) => void }) {
  const org = useOrg();
  const { data, isPending } = useQuery(factoriesQuery(org.id));
  const headingId = useId();
  const published = (data ?? []).filter((f) => f.current).toSorted((a, b) => a.name.localeCompare(b.name));

  if (isPending) return <div aria-busy="true" className="h-48 rounded-card border border-line bg-card" />;
  if (published.length === 0) {
    return (
      <EmptyState icon={<Copy size={20} strokeWidth={1.5} />} title={`Nothing in ${org.name} to copy yet`}>
        Only a published factory can be cloned. Start blank or from a template instead.
      </EmptyState>
    );
  }

  return (
    <section
      aria-labelledby={headingId}
      className="flex max-w-3xl flex-col gap-4 rounded-card border border-line bg-card p-4 md:p-5"
    >
      <div>
        <h2 id={headingId} className="m-0 text-xl font-medium tracking-[-0.01em]">
          Factories to copy
        </h2>
        <p className="m-0 mt-1 text-sm text-muted">
          Copies the line, agents and prompts of the version that runs, without items, history or credentials.
        </p>
      </div>
      <RadioList
        legend="Factory to copy"
        name="clone"
        value={value}
        onChange={onChange}
        options={published.map((f) => ({
          value: f.id,
          label: <span translate="no">{f.name}</span>,
          description: f.description || f.title,
          aside: `v${f.current?.number}, ${plural(f.stations.length, 'station')}`,
        }))}
      />
    </section>
  );
}

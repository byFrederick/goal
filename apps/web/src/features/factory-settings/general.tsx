// General: the factory's name, purpose and badge, read-only. The badge is the one the sidebar draws; no
// definition field stores a colour or letter, so it cannot be edited here (ADR 0016, decision 4).
import { useQuery } from '@tanstack/react-query';
import { factoriesQuery } from '../../api/queries';
import type { Factory } from '../../api/types';
import { FactoryBadge } from '../../app/factory-badge';
import { factoryMark } from '../../shared/marks';
import { useOrg } from '../../shared/org';
import { SectionCard } from '../../ui/card';
import { Field, Input, Textarea } from '../../ui/field';

const rowClass = 'border-t border-line pt-4 md:grid md:grid-cols-[200px_minmax(0,1fr)] md:gap-x-5 md:gap-y-2';

export function GeneralCard({ factory }: { factory: Factory }) {
  const org = useOrg();
  const { data: factories = [] } = useQuery(factoriesQuery(org.id));
  return (
    <SectionCard id="general" title="General">
      <Field label="Name" className={rowClass}>
        <Input readOnly value={factory.name} />
      </Field>
      <Field label="Purpose" className={rowClass} help="Agents read this as their first line of context.">
        <Textarea readOnly rows={2} value={factory.description ?? ''} />
      </Field>
      <div className={rowClass}>
        <span className="text-sm font-medium">Badge</span>
        <div className="flex items-center gap-3">
          <FactoryBadge mark={factoryMark(factory, factories)} className="size-8" />
        </div>
      </div>
    </SectionCard>
  );
}

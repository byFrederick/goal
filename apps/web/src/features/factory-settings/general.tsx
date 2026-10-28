// General: the factory's name and purpose, read-only. The definition holds the rest of the screen's fields
// (badge, handle, repository, runtime, budgets, flow), and no endpoint writes these two yet, so they only show.
import type { Factory } from '../../api/types';
import { SectionCard } from '../../ui/card';
import { Field, Input, Textarea } from '../../ui/field';

const rowClass = 'border-t border-line pt-4 md:grid md:grid-cols-[200px_minmax(0,1fr)] md:gap-x-5 md:gap-y-2';

export function GeneralCard({ factory }: { factory: Factory }) {
  return (
    <SectionCard id="general" title="General">
      <Field label="Name" className={rowClass}>
        <Input readOnly value={factory.name} />
      </Field>
      <Field label="Purpose" className={rowClass} help="Agents read this as their first line of context.">
        <Textarea readOnly rows={2} value={factory.description ?? ''} />
      </Field>
    </SectionCard>
  );
}

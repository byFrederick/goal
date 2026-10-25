// Basics (D §10.5, spec 4 §10, canvas Define-Basics): name, title, purpose, work types, the
// runtime (model, model connection, size, tools, repository and its connection), who approves
// (spec 3 §10.2) and, in Definition, the factory id. Two columns that become one under 640 px of
// `main`. Every change is a session edit (typing into one field is one undo step) saved 800 ms
// later; leaving a field saves at once. B's problems show at their field.
import { Copy } from 'lucide-react';
import { toast } from 'sonner';
import { Field, Input, Textarea } from '../../../ui/field';
import { setBasics } from '../draft/ops';
import { useDraftSession, useSection } from '../draft/use-draft';
import { EditedMark, SectionShell } from '../frame/section-shell';
import { useProblemText } from '../frame/use-editor';
import type { SectionViewProps } from '../section-views';
import { RuntimeFields } from './runtime-fields';
import { WhoApproves } from './who-approves';
import { WorkTypes } from './work-types';

type TextField = 'name' | 'title' | 'purpose';

export function BasicsSection({ place }: SectionViewProps) {
  const session = useDraftSession();
  const basics = useSection('basics');
  const problems = {
    name: useProblemText('/basics/name'),
    title: useProblemText('/basics/title'),
    purpose: useProblemText('/basics/purpose'),
  };

  const text = (field: TextField, required = false) => ({
    value: basics[field] ?? '',
    onChange: (e: { target: { value: string } }) => {
      const value = e.target.value;
      session.edit(setBasics(basics, field, value === '' && !required ? undefined : value), {
        coalesce: `/basics/${field}`,
      });
    },
    onBlur: () => void session.flush(),
  });
  const edited = (field: TextField, label: string) => (
    <EditedMark section="basics" pointer={`/basics/${field}`} label={label} />
  );

  return (
    <SectionShell section="basics">
      <div className="grid grid-cols-1 gap-x-5 gap-y-4 @[640px]:grid-cols-2">
        <Field
          label="Name"
          aside={edited('name', 'the name')}
          error={problems.name}
          help="Lowercase, used in links and the rail"
        >
          <Input
            {...text('name', true)}
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            translate="no"
            className="font-mono md:text-sm"
          />
        </Field>
        <Field
          label="Title"
          aside={edited('title', 'the title')}
          error={problems.title}
          help="Shown on the Floor and in notifications"
        >
          <Input {...text('title')} autoComplete="off" />
        </Field>
        <Field
          label="Purpose"
          aside={edited('purpose', 'the purpose')}
          error={problems.purpose}
          help="One or two sentences. Agents read this before every item."
          className="col-span-full"
        >
          <Textarea {...text('purpose')} rows={2} className="field-sizing-content min-h-[4.5rem]" />
        </Field>
        <WorkTypes basics={basics} />
        <RuntimeFields basics={basics} />
        <WhoApproves place={place} factory={basics.id} />
        {place === 'definition' && basics.id ? <FactoryId id={basics.id} /> : null}
      </div>
    </SectionShell>
  );
}

function FactoryId({ id }: { id: string }) {
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(id);
      toast.success('Factory id copied');
    } catch {
      toast.error('The factory id could not be copied');
    }
  };
  return (
    <div className="col-span-full flex min-w-0 flex-col gap-1.5">
      <span className="text-sm font-medium">Factory id</span>
      <span className="flex min-w-0 items-center gap-2">
        <span translate="no" className="font-mono text-sm break-all">
          {id}
        </span>
        <button
          type="button"
          aria-label="Copy factory id"
          onClick={() => void copy()}
          className="inline-flex size-11 shrink-0 items-center justify-center rounded-control border border-line bg-surface text-ink transition-colors duration-200 hover:bg-shell md:size-8"
        >
          <Copy size={15} strokeWidth={1.5} aria-hidden="true" />
        </button>
      </span>
      <p className="m-0 text-[13px] leading-snug text-muted">
        Never changes, even if you rename the factory. Items, traces and spend keep pointing here.
      </p>
    </div>
  );
}

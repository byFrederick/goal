// Basics › Advanced (D §10.5), closed at first: how agents reach Claude (read-only until
// credentials arrive) and the default model (B's agents.default_model). Runner, image and
// knowledge collections are edited in YAML.
import { useQuery } from '@tanstack/react-query';
import { ChevronRight } from 'lucide-react';
import { builderMetaQuery } from '../../../api/queries';
import { Field, NativeSelect } from '../../../ui/field';
import { setDefaultModel } from '../draft/ops';
import { useDraftSession, useSection } from '../draft/use-draft';
import { EditedMark } from '../frame/section-shell';
import { useProblemText } from '../frame/use-editor';

export function AdvancedFields() {
  const session = useDraftSession();
  const agents = useSection('agents');
  const meta = useQuery(builderMetaQuery(session.org));
  const problem = useProblemText('/agents/default_model');
  const models = meta.data?.models ?? [];
  const fallback = models.find((m) => m.default);
  const current = agents.default_model ?? '';
  const known = current === '' || models.some((m) => m.id === current);

  return (
    <details className="group col-span-full rounded-control border border-line bg-surface">
      <summary className="flex min-h-11 list-none items-center gap-2 px-3 text-sm font-medium [&::-webkit-details-marker]:hidden">
        <ChevronRight
          size={16}
          strokeWidth={1.75}
          aria-hidden="true"
          className="transition-transform duration-200 group-open:rotate-90"
        />
        Advanced
      </summary>
      <div className="grid gap-4 border-t border-line p-3 @[640px]:grid-cols-2 @[640px]:gap-x-5">
        <div className="flex min-w-0 flex-col gap-1.5">
          <span className="text-sm font-medium">Model access</span>
          <p className="m-0 text-[15px] leading-normal">This install’s model access (a platform setting)</p>
          <p className="m-0 text-[13px] leading-snug text-muted">
            Choosing a subscription or API key arrives with credentials.
          </p>
        </div>
        <Field
          label="Default model"
          aside={<EditedMark section="agents" pointer="/agents/default_model" label="the default model" />}
          error={problem}
          help="For agents that don’t name a model. Runner, image and knowledge collections are edited in YAML."
        >
          <NativeSelect
            value={current}
            onChange={(e) => session.edit(setDefaultModel(agents, e.target.value === '' ? undefined : e.target.value))}
          >
            <option value="">{fallback ? `goald’s default (${fallback.label})` : 'goald’s default'}</option>
            {known ? null : <option value={current}>{current}</option>}
            {models.map((m) => (
              <option key={m.id} value={m.id}>
                {m.label}
              </option>
            ))}
          </NativeSelect>
        </Field>
      </div>
    </details>
  );
}

// The Models box of the Line step (line.html's "Models" box): the agents and the model each runs on, one select per
// agent, and the model preset control on the heading row. Shared by the onboarding Line step and the new-factory
// wizard's Line step; each passes its own preset control.
import { useQuery } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { builderMetaQuery } from '../../../api/queries';
import { NativeSelect } from '../../../ui/field';
import { setAgentField } from '../draft/ops';
import { useDraftSession, useSection } from '../draft/use-draft';
import { useLineEditor } from './line-editor';

export function ModelsBox({ preset }: { preset: ReactNode }) {
  const editor = useLineEditor();
  const session = useDraftSession();
  const agents = useSection('agents');
  const meta = useQuery(builderMetaQuery(session.org));
  const models = meta.data?.models ?? [];
  const fallback = agents.default_model ?? models.find((m) => m.default)?.id;

  return (
    <section aria-labelledby="models-heading" className="flex flex-col rounded-card border border-line bg-card">
      <div className="flex items-center justify-between gap-3 px-4 py-3">
        <h2 id="models-heading" className="m-0 text-body font-medium">
          Models
        </h2>
        {preset}
      </div>
      {(agents.agents ?? []).length === 0 ? (
        <p className="m-0 border-t border-line px-4 py-4 text-secondary text-muted">No agents yet.</p>
      ) : (
        <ul className="m-0 list-none p-0">
          {(agents.agents ?? []).map((agent) => {
            const current = agent.model ?? '';
            const known = current === '' || models.some((m) => m.id === current);
            return (
              <li key={agent.name} className="flex flex-wrap items-center gap-3 border-t border-line px-4 py-3">
                <span className="min-w-0 flex-1 font-control text-body break-all">{agent.name}</span>
                <NativeSelect
                  aria-label={`Model of ${agent.name}`}
                  value={current}
                  disabled={editor.viewOnly}
                  onChange={(e) =>
                    session.edit(setAgentField(agents, agent.name, 'model', e.target.value || undefined))
                  }
                  className="w-full font-mono md:w-auto md:text-sm"
                >
                  <option value="">{fallback ? `Factory default (${fallback})` : 'Factory default'}</option>
                  {known ? null : <option value={current}>{current}</option>}
                  {models.map((m) => (
                    <option key={m.id} value={m.id}>
                      {m.id}
                    </option>
                  ))}
                </NativeSelect>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}

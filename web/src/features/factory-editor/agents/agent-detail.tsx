// One agent (D §10.7): its name and where it works, Duplicate / Rename… / Remove…, then Role,
// Model, Max turns and Cap per item, the prompt, its skills, its built-in tools and the factory
// rules that apply to it (agent-rules.tsx). Every field
// is a session edit (typing into one field is one undo step), saved 800 ms later or on leaving it.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { ChevronRight } from 'lucide-react';
import { useId, useState } from 'react';
import { toast } from 'sonner';
import { builderMetaQuery } from '../../../api/queries';
import type { ModelAgent } from '../../../shared/definition/model';
import { whereLabel } from '../../../shared/definition/where';
import { useFactoryParams } from '../../../shared/factory';
import { formatClock } from '../../../shared/format';
import { Button } from '../../../ui/button';
import { Field, Input, NativeSelect } from '../../../ui/field';
import { duplicateAgent, setAgentField } from '../draft/ops';
import { useDraft, useDraftSession, useSection, useViewOnly } from '../draft/use-draft';
import { EditedMark } from '../frame/section-shell';
import { useProblemText } from '../frame/use-editor';
import { useSetSearch } from '../search-link';
import type { SectionViewProps } from '../section-views';
import { RemoveAgentDialog, RenameAgentDialog } from './agent-dialogs';
import { AgentRules } from './agent-rules';
import { BuiltinsEditor } from './builtins-editor';
import { PromptEditor, usePromptText } from './prompt-editor';
import { SkillsEditor } from './skills-editor';

interface AgentDetailProps {
  agent: ModelAgent;
  place: SectionViewProps['place'];
  /** `?add=skill`: the skill picker opens, focused. */
  openPicker: boolean;
}

export function AgentDetail({ agent, place, openPicker }: AgentDetailProps) {
  const session = useDraftSession();
  const viewOnly = useViewOnly();
  const agents = useSection('agents');
  const line = useSection('line');
  const savedAt = useDraft((s) => (s.hasDraft ? s.savedAt : null));
  const setSearch = useSetSearch();
  const prompt = usePromptText(agent.name);
  const [renaming, setRenaming] = useState(false);
  const [removing, setRemoving] = useState(false);
  const index = (agents.agents ?? []).findIndex((a) => a.name === agent.name);
  const at = `/agents/agents/${index}`;
  const where = whereLabel(line, agent.name);
  const capHelp = useId();

  const duplicate = async () => {
    try {
      const text = await prompt.load();
      const edit = duplicateAgent(agents, agent.name, text);
      session.edit(edit);
      const copy = edit.value.agents?.at(-1)?.name;
      if (copy) setSearch({ agent: copy, add: undefined });
    } catch {
      toast.error('The prompt could not be read, so nothing was copied. Try again.');
    }
  };

  return (
    <section
      aria-labelledby="agent-detail-name"
      className="flex min-w-0 flex-col gap-[18px] rounded-card border border-line bg-card p-4 md:p-5"
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 flex-[1_1_260px]">
          <h3
            id="agent-detail-name"
            translate="no"
            className="m-0 text-[28px] leading-tight font-normal tracking-display break-words"
          >
            {agent.name}
          </h3>
          <p className="m-0 mt-1 text-sm text-muted">
            {where === 'Not on the line' ? 'Not on the line yet: give it a station in Line.' : `Works at ${where}.`}
            {savedAt ? ` Saved with the draft at ${formatClock(savedAt)}.` : ''}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          {place === 'definition' ? <AgentPageLink agent={agent.name} /> : null}
          {viewOnly ? null : (
            <>
              <Button variant="white" size="sm" className="h-11 md:h-8" onClick={() => void duplicate()}>
                Duplicate
              </Button>
              <Button variant="white" size="sm" className="h-11 md:h-8" onClick={() => setRenaming(true)}>
                Rename…
              </Button>
              <Button variant="white" size="sm" className="h-11 md:h-8" onClick={() => setRemoving(true)}>
                Remove…
              </Button>
            </>
          )}
        </div>
      </div>
      <RenameAgentDialog agent={agent} open={renaming} onOpenChange={setRenaming} />
      <RemoveAgentDialog agent={agent} open={removing} onOpenChange={setRemoving} />

      <div className="grid grid-cols-1 gap-3.5 @[640px]:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)]">
        <Field
          label="Role"
          aside={<EditedMark section="agents" pointer={`${at}/description`} label={`the role of ${agent.name}`} />}
          error={useProblemText(`${at}/description`)}
        >
          <Input
            value={agent.description ?? ''}
            autoComplete="off"
            onChange={(e) =>
              session.edit(setAgentField(agents, agent.name, 'description', e.target.value || undefined), {
                coalesce: `${at}/description`,
              })
            }
            onBlur={() => void session.flush()}
          />
        </Field>
        <ModelField agent={agent} at={at} />
        <div className="grid grid-cols-2 gap-x-3 gap-y-1.5">
          <NumberField
            label="Max turns"
            value={agent.max_turns}
            pointer={`${at}/max_turns`}
            editedLabel={`the max turns of ${agent.name}`}
            integer
            onCommit={(n, coalesce) => session.edit(setAgentField(agents, agent.name, 'max_turns', n), { coalesce })}
          />
          <NumberField
            label="Cap per item"
            value={agent.per_item_usd}
            pointer={`${at}/per_item_usd`}
            editedLabel={`the cap per item of ${agent.name}`}
            prefix="$"
            describedBy={capHelp}
            onCommit={(n, coalesce) => session.edit(setAgentField(agents, agent.name, 'per_item_usd', n), { coalesce })}
          />
        </div>
      </div>

      <PromptEditor agent={agent} index={index} />

      <div className="grid grid-cols-1 items-start gap-5 @[1000px]:grid-cols-2">
        <SkillsEditor agent={agent} index={index} openPicker={openPicker} />
        <BuiltinsEditor agent={agent} index={index} />
      </div>

      <AgentRules agent={agent.name} />
    </section>
  );
}

/** Definition only: the agent's page (E's /agents/$agent), its statistics and cards. */
function AgentPageLink({ agent }: { agent: string }) {
  const params = useFactoryParams();
  return (
    <Button asChild variant="white" size="sm" className="h-11 md:h-8">
      <Link to="/o/$org/f/$factory/agents/$agent" params={{ ...params, agent }}>
        Open agent page
        <ChevronRight size={14} strokeWidth={1.5} aria-hidden="true" />
      </Link>
    </Button>
  );
}

function ModelField({ agent, at }: { agent: ModelAgent; at: string }) {
  const session = useDraftSession();
  const agents = useSection('agents');
  const meta = useQuery(builderMetaQuery(session.org));
  const models = meta.data?.models ?? [];
  const fallback = agents.default_model ?? models.find((m) => m.default)?.id;
  const current = agent.model ?? '';
  const known = current === '' || models.some((m) => m.id === current);
  return (
    <Field
      label="Model"
      aside={<EditedMark section="agents" pointer={`${at}/model`} label={`the model of ${agent.name}`} />}
      error={useProblemText(`${at}/model`)}
    >
      <NativeSelect
        value={current}
        onChange={(e) => session.edit(setAgentField(agents, agent.name, 'model', e.target.value || undefined))}
        className="font-mono md:text-sm"
      >
        <option value="">{fallback ? `Factory default (${fallback})` : 'Factory default'}</option>
        {known ? null : <option value={current}>{current}</option>}
        {models.map((m) => (
          <option key={m.id} value={m.id}>
            {m.id}
          </option>
        ))}
      </NativeSelect>
    </Field>
  );
}

/**
 * A number the agent may leave unset: what is typed stays while the field has focus (so "1." or
 * a typo isn't replaced), each valid value is an edit, and an empty field removes the setting.
 */
function NumberField({
  label,
  value,
  pointer,
  editedLabel,
  integer = false,
  prefix,
  describedBy,
  onCommit,
}: {
  label: string;
  value: number | undefined;
  pointer: string;
  editedLabel: string;
  integer?: boolean;
  prefix?: string;
  /** A help line outside the field (shared by two fields). */
  describedBy?: string;
  onCommit: (value: number | undefined, coalesce: string) => void;
}) {
  const session = useDraftSession();
  const [typed, setTyped] = useState<string | null>(null);
  const [invalid, setInvalid] = useState<string>();
  const problem = useProblemText(pointer);
  const shown = typed ?? (value === undefined ? '' : integer ? String(value) : value.toFixed(2));

  const change = (text: string) => {
    setTyped(text);
    const clean = text.trim();
    if (clean === '') {
      setInvalid(undefined);
      onCommit(undefined, pointer);
      return;
    }
    const n = Number(clean);
    if (!Number.isFinite(n) || n < (integer ? 1 : 0) || (integer && !Number.isInteger(n))) {
      setInvalid(integer ? 'A whole number, 1 or more.' : 'An amount in dollars, such as 1.50.');
      return;
    }
    setInvalid(undefined);
    onCommit(n, pointer);
  };

  return (
    <Field
      label={label}
      aside={<EditedMark section="agents" pointer={pointer} label={editedLabel} />}
      error={invalid ?? problem}
    >
      <span className="relative flex">
        {prefix ? (
          <span aria-hidden="true" className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-muted">
            {prefix}
          </span>
        ) : null}
        <Input
          value={shown}
          inputMode={integer ? 'numeric' : 'decimal'}
          autoComplete="off"
          placeholder={integer ? 'Default' : 'None'}
          // The Field links its error; without one, the shared help line describes the field.
          {...(describedBy && !(invalid ?? problem) ? { 'aria-describedby': describedBy } : {})}
          onChange={(e) => change(e.target.value)}
          onBlur={() => {
            setTyped(null);
            setInvalid(undefined);
            void session.flush();
          }}
          className={prefix ? 'pl-6 placeholder:text-muted' : 'placeholder:text-muted'}
        />
      </span>
    </Field>
  );
}

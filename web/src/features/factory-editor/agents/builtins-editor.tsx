// An agent's built-in tools (D §10.7, B's `builtins`): five rows, each Off / Ask me / Allowed.
// Run commands lists the commands it may start ("Limit to commands…"). "Ask me" stays selectable;
// until tool approvals exist B warns that it is asked as Off, under the row. An agent with a
// legacy tools block shows "Custom rules, edit in YAML" and can switch to these settings.
import { useQuery } from '@tanstack/react-query';
import { type FormEvent, useId, useRef, useState } from 'react';
import { builderMetaQuery } from '../../../api/queries';
import type { BuiltinKey, BuiltinMode, Builtins, ModelAgent } from '../../../shared/definition/model';
import { builtinLabels, defaultBuiltins } from '../../../shared/definition/tool-policy';
import { Button } from '../../../ui/button';
import { ConfirmDialog } from '../../../ui/confirm-dialog';
import { Dialog, DialogClose, DialogContent } from '../../../ui/dialog';
import { Field, Input } from '../../../ui/field';
import { SegmentedControl } from '../../../ui/segmented-control';
import { setAgentField, setBuiltin, setRunCommands, useBuiltins as simpleBuiltins } from '../draft/ops';
import { useDraft, useDraftSession, useSection } from '../draft/use-draft';
import { useProblems } from '../frame/use-editor';
import { SearchLink } from '../search-link';

const modes = [
  { value: 'off', label: 'Off' },
  { value: 'ask', label: 'Ask me' },
  { value: 'allowed', label: 'Allowed' },
] as const satisfies readonly { value: BuiltinMode; label: string }[];

const and = (words: string[]) =>
  words.length < 2 ? (words[0] ?? '') : `${words.slice(0, -1).join(', ')} and ${words.at(-1)}`;

const rows: { key: BuiltinKey; label: string; detail: (b: Builtins) => string }[] = [
  { key: 'read', label: builtinLabels.read, detail: () => 'In the item workspace' },
  { key: 'edit', label: builtinLabels.edit, detail: () => 'In the item workspace' },
  {
    key: 'run',
    label: builtinLabels.run,
    detail: (b) => {
      const commands = b.run?.commands ?? [];
      return commands.length > 0 ? `Only ${and(commands)}` : 'Any command in the item workspace';
    },
  },
  { key: 'web', label: builtinLabels.web, detail: () => 'Public pages only' },
  { key: 'subagents', label: builtinLabels.subagents, detail: () => 'Uses more Claude slots' },
];

const modeOf = (b: Builtins, key: BuiltinKey): BuiltinMode => (key === 'run' ? b.run?.mode : b[key]) ?? 'off';

export function BuiltinsEditor({ agent, index }: { agent: ModelAgent; index: number }) {
  const session = useDraftSession();
  const agents = useSection('agents');
  const headingId = useId();
  const meta = useQuery(builderMetaQuery(session.org)).data;
  const problems = useProblems(`/agents/agents/${index}/builtins`);
  const [limiting, setLimiting] = useState(false);
  const builtins = agent.builtins ?? defaultBuiltins;

  const choose = (key: BuiltinKey, mode: BuiltinMode) => {
    const from = agent.builtins ? agents : setAgentField(agents, agent.name, 'builtins', defaultBuiltins).value;
    session.edit(setBuiltin(from, agent.name, key, mode));
  };

  return (
    <div className="flex min-w-0 flex-col">
      <h4 id={headingId} className="m-0 mb-1 text-[15px] font-medium">
        Built-in tools
      </h4>
      <p className="m-0 mb-1.5 text-[13px] text-muted">
        Tools from GitHub, browsers and other systems are set in Connections.
      </p>
      {agent.custom_tools ? (
        <CustomRules agent={agent} index={index} />
      ) : (
        <fieldset aria-labelledby={headingId} className="m-0 min-w-0 border-0 p-0">
          {rows.map((row) => {
            const warning = meta?.capabilities.tool_approvals
              ? undefined
              : problems.find(
                  (p) =>
                    p.code === 'builtin-ask-unavailable' && p.pointer === `/agents/agents/${index}/builtins/${row.key}`,
                );
            const warningId = `${headingId}-${row.key}-ask`;
            return (
              <div key={row.key} className="flex flex-col gap-1.5 border-t border-line py-2.5">
                <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
                  <span className="min-w-0">
                    <span className="block text-sm font-medium">{row.label}</span>
                    <span className="block text-[13px] text-muted">{row.detail(builtins)}</span>
                    {row.key === 'run' && modeOf(builtins, 'run') !== 'off' ? (
                      <button
                        type="button"
                        onClick={() => setLimiting(true)}
                        className="min-h-6 text-[13px] text-ink underline underline-offset-3 transition-colors duration-200 hover:text-run"
                      >
                        Limit to commands…
                      </button>
                    ) : null}
                  </span>
                  <SegmentedControl
                    label={row.label}
                    options={modes}
                    value={modeOf(builtins, row.key)}
                    onChange={(mode) => choose(row.key, mode)}
                    describedBy={warning ? warningId : undefined}
                  />
                </div>
                {warning ? (
                  <p id={warningId} className="m-0 text-[13px] leading-snug text-amber-ink">
                    Asked as Off until tool approvals arrive
                  </p>
                ) : null}
              </div>
            );
          })}
        </fieldset>
      )}
      <CommandsDialog
        open={limiting}
        onOpenChange={setLimiting}
        commands={builtins.run?.commands ?? []}
        onSave={(commands) => {
          const from = agent.builtins ? agents : setAgentField(agents, agent.name, 'builtins', defaultBuiltins).value;
          session.edit(setRunCommands(from, agent.name, commands));
        }}
      />
    </div>
  );
}

/** A legacy `tools` block: its rules stay in YAML until the agent switches to simple settings. */
function CustomRules({ agent, index }: { agent: ModelAgent; index: number }) {
  const session = useDraftSession();
  const agents = useSection('agents');
  const line = useDraft((s) => s.ranges[`/agents/agents/${index}/custom_tools`]?.[0]);
  const [confirming, setConfirming] = useState(false);
  return (
    <div className="flex flex-col items-start gap-2 border-t border-line py-2.5 text-sm">
      <p className="m-0">Custom rules, edit in YAML.</p>
      <div className="flex flex-wrap gap-2">
        <SearchLink
          patch={{ mode: 'yaml', ...(line ? { line } : {}) }}
          className="inline-flex min-h-11 items-center text-ink underline underline-offset-3 hover:text-run md:min-h-8"
        >
          Open in YAML
        </SearchLink>
        <Button variant="white" size="sm" className="h-11 md:h-8" onClick={() => setConfirming(true)}>
          Switch to simple settings…
        </Button>
      </div>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={`Switch ${agent.name} to simple settings?`}
        description="Its custom rules are replaced by the five built-in tools, all off. Undo puts the rules back."
        confirmLabel="Switch"
        onConfirm={() => {
          session.edit(simpleBuiltins(agents, agent.name));
          setConfirming(false);
        }}
      />
    </div>
  );
}

/** "Limit to commands…": the commands Run commands may start; none means any. */
function CommandsDialog({
  open,
  onOpenChange,
  commands,
  onSave,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  commands: string[];
  onSave: (commands: string[]) => void;
}) {
  const [text, setText] = useState(commands.join(', '));
  const input = useRef<HTMLInputElement>(null);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    e.stopPropagation();
    const list = [...new Set(text.split(/[\s,]+/).filter(Boolean))];
    onSave(list);
    onOpenChange(false);
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next) setText(commands.join(', '));
        onOpenChange(next);
      }}
    >
      <DialogContent
        title="Limit to commands"
        description="The agent may start only these programs. Leave it empty to allow any command."
        initialFocus={input}
      >
        <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
          <Field label="Commands" help="Separated by commas or spaces: terraform, tflint, git">
            <Input
              ref={input}
              value={text}
              onChange={(e) => setText(e.target.value)}
              autoComplete="off"
              autoCapitalize="off"
              spellCheck={false}
              className="font-mono md:text-sm"
            />
          </Field>
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary" size="lg" className="md:h-10 md:text-sm">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" variant="primary" size="lg" className="md:h-10 md:text-sm">
              Save commands
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

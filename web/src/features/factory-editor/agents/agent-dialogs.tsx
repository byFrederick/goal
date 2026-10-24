// The agents list's and the detail's dialogs (D §10.7): Add agent (a name; the agent and its
// prompt file in one save), Copy from another factory (an agent of another factory's running
// version with its prompt and skill folders, without connections), Rename… (every reference and
// the prompt file move with it) and Remove… (blocked while stations use the agent).
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Copy, Plus } from 'lucide-react';
import { type FormEvent, type ReactNode, useRef, useState } from 'react';
import { toast } from 'sonner';
import { ApiError } from '../../../api/client';
import { getVersionFile } from '../../../api/definitions';
import { factoriesQuery, versionFileQuery, versionModelQuery, versionQuery } from '../../../api/queries';
import type { FileMap } from '../../../api/types';
import { type ModelAgent, modelOf } from '../../../shared/definition/model';
import { cleanPath, promptFile } from '../../../shared/definition/place';
import { stationsUsing } from '../../../shared/definition/where';
import { Button } from '../../../ui/button';
import { ConfirmDialog } from '../../../ui/confirm-dialog';
import { Dialog, DialogClose, DialogContent } from '../../../ui/dialog';
import { Field, Input, NativeSelect } from '../../../ui/field';
import { AgentInUseError, addAgent, type CopiedSkill, copyAgent, removeAgent, renameAgent } from '../draft/ops';
import { useDraftSession, useModel, useSection } from '../draft/use-draft';
import { SectionLink, useSetSearch } from '../search-link';
import { usePromptText } from './prompt-editor';

const agentName = /^[a-z][a-z0-9-]{1,38}[a-z0-9]$/;

/** Why a name can't be an agent's, or undefined when it can. */
export function agentNameError(name: string, taken: readonly string[]): string | undefined {
  if (name === '') return 'Give the agent a name.';
  if (!agentName.test(name)) {
    return 'Lowercase letters, digits and dashes, 3 to 40 characters, starting with a letter: cost-analyst.';
  }
  if (taken.includes(name)) return `${name} is already an agent of this factory.`;
  return undefined;
}

const errorText = (err: unknown) =>
  err instanceof ApiError ? (err.detail ?? err.title) : 'goald can’t be reached. Try again.';

/** A dialog with one name field: Add agent and Rename…. */
function NameDialog({
  open,
  onOpenChange,
  title,
  description,
  submitLabel,
  initial,
  taken,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: ReactNode;
  submitLabel: string;
  initial: string;
  taken: readonly string[];
  onSubmit: (name: string) => Promise<void>;
}) {
  const [name, setName] = useState(initial);
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState(false);
  const input = useRef<HTMLInputElement>(null);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    e.stopPropagation();
    const value = name.trim();
    const problem = agentNameError(value, taken);
    setError(problem);
    if (problem) {
      input.current?.focus();
      return;
    }
    setPending(true);
    try {
      await onSubmit(value);
      onOpenChange(false);
    } catch (err) {
      setError(errorText(err));
    } finally {
      setPending(false);
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next) {
          setName(initial);
          setError(undefined);
        }
        onOpenChange(next);
      }}
    >
      <DialogContent title={title} description={description} initialFocus={input}>
        <form onSubmit={(e) => void submit(e)} className="flex flex-col gap-4" noValidate>
          <Field label="Name" error={error} help="Lowercase, used in the line and in file names">
            <Input
              ref={input}
              value={name}
              onChange={(e) => {
                setName(e.target.value);
                setError(undefined);
              }}
              autoComplete="off"
              autoCapitalize="off"
              spellCheck={false}
              translate="no"
              className="font-mono md:text-sm"
            />
          </Field>
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary" size="lg" className="md:h-10 md:text-sm">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" variant="primary" size="lg" className="md:h-10 md:text-sm" disabled={pending}>
              {pending ? 'Saving…' : submitLabel}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

const listButton = 'h-11 w-full justify-center md:h-10';

/** Add agent: the agent with `prompt: ./agents/<name>.md` and that file, then its detail. */
export function AddAgentButton() {
  const session = useDraftSession();
  const agents = useSection('agents');
  const setSearch = useSetSearch();
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button variant="white" className={listButton} onClick={() => setOpen(true)}>
        <Plus size={16} strokeWidth={1.5} aria-hidden="true" />
        Add agent
      </Button>
      <NameDialog
        open={open}
        onOpenChange={setOpen}
        title="Add agent"
        description="It gets a prompt file, agents/<name>.md, to write next."
        submitLabel="Add agent"
        initial=""
        taken={(agents.agents ?? []).map((a) => a.name)}
        onSubmit={async (name) => {
          session.edit(addAgent(agents, name));
          // The prompt file exists once the save lands: then the editor can load it.
          await session.flush();
          setSearch({ agent: name, add: undefined });
        }}
      />
    </>
  );
}

/** Rename…: stations, lanes, the librarian and connections follow; the prompt file moves. */
export function RenameAgentDialog({
  agent,
  open,
  onOpenChange,
}: {
  agent: ModelAgent;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const session = useDraftSession();
  const model = useModel();
  const setSearch = useSetSearch();
  const prompt = usePromptText(agent.name);
  return (
    <NameDialog
      open={open}
      onOpenChange={onOpenChange}
      title={`Rename ${agent.name}`}
      description="Stations, lanes and connections that name it follow, and its prompt file moves."
      submitLabel="Rename"
      initial={agent.name}
      taken={(model.agents.agents ?? []).map((a) => a.name)}
      onSubmit={async (name) => {
        // Unsaved prompt typing goes first, so the moved file carries it.
        await session.flush();
        const text = prompt.text ?? (await prompt.load());
        session.edit(renameAgent(model, agent.name, name, text));
        setSearch({ agent: name }, { replace: true });
      }}
    />
  );
}

/** Remove…: blocked while stations use the agent (listed as links to them); else confirmed. */
export function RemoveAgentDialog({
  agent,
  open,
  onOpenChange,
}: {
  agent: ModelAgent;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const session = useDraftSession();
  const model = useModel();
  const setSearch = useSetSearch();
  const using = stationsUsing(model.line, agent.name);

  if (using.length > 0) {
    return (
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent
          title={`${agent.name} can’t be removed yet`}
          description="Stations still use it. Give them another agent in Line first."
        >
          <ul className="m-0 flex list-none flex-col gap-1 p-0 text-[15px]">
            {using.map((s) => (
              <li key={s.id}>
                <SectionLink
                  section="line"
                  patch={{ station: s.id, agent: undefined, add: undefined }}
                  className="text-ink underline underline-offset-3 hover:text-run"
                >
                  {s.name || s.id}
                </SectionLink>
              </li>
            ))}
          </ul>
          <div className="flex justify-end">
            <DialogClose asChild>
              <Button variant="secondary" size="lg" className="md:h-10 md:text-sm">
                Close
              </Button>
            </DialogClose>
          </div>
        </DialogContent>
      </Dialog>
    );
  }

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      tone="danger"
      title={`Remove ${agent.name}?`}
      description="Its prompt file goes with it, and connections stop listing it. Undo puts it back."
      confirmLabel="Remove agent"
      onConfirm={() => {
        try {
          session.edit(removeAgent(model, agent.name));
        } catch (err) {
          if (!(err instanceof AgentInUseError)) throw err;
          return;
        }
        onOpenChange(false);
        setSearch({ agent: undefined, add: undefined });
        toast.success(`${agent.name} removed`);
      }}
    />
  );
}

/** Copy from another factory: pick a factory and one of its agents, preview it, then copy. */
export function CopyAgentButton() {
  const session = useDraftSession();
  const client = useQueryClient();
  const agents = useSection('agents');
  const setSearch = useSetSearch();
  const [open, setOpen] = useState(false);
  const [factory, setFactory] = useState('');
  const [agent, setAgent] = useState('');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string>();

  const factories = useQuery({ ...factoriesQuery(session.org), enabled: open }).data ?? [];
  const others = factories.filter((f) => f.id !== session.factory && f.current);
  const source = useQuery({ ...versionModelQuery(session.org, factory, 'current'), enabled: open && factory !== '' });
  const model = source.data ? modelOf(source.data.model) : undefined;
  const sourceAgents = model?.agents.agents ?? [];
  const picked = sourceAgents.find((a) => a.name === agent);
  const path = model && picked ? promptFile(model, picked.name) : undefined;
  const preview = useQuery({
    ...versionFileQuery(session.org, factory, 'current', path ?? '', picked?.prompt?.sha256 ?? ''),
    enabled: path !== undefined && picked?.prompt?.sha256 !== undefined,
  });

  const reset = () => {
    setFactory('');
    setAgent('');
    setError(undefined);
  };

  const copy = async () => {
    if (!model || !picked) return;
    setPending(true);
    setError(undefined);
    try {
      const [prompt, version] = await Promise.all([
        path ? getVersionFile(session.org, factory, 'current', path).then((f) => f.text) : Promise.resolve(''),
        client.fetchQuery(versionQuery(session.org, factory, 'current')),
      ]);
      const skills: CopiedSkill[] = await Promise.all(
        (model.agents.skills ?? [])
          .filter((s) => picked.skills?.includes(s.name))
          .map(async (skill) => {
            const folder = `${cleanPath(skill.source)}/`;
            const files: FileMap = {};
            if (skill.source.startsWith('./')) {
              await Promise.all(
                version.files
                  .filter((f) => f.path.startsWith(folder))
                  .map(async (f) => {
                    const file = await getVersionFile(session.org, factory, 'current', f.path);
                    files[f.path.slice(folder.length)] = { text: file.text };
                  }),
              );
            }
            return { skill, files };
          }),
      );
      const { edit, name } = copyAgent(agents, picked, prompt, skills);
      session.edit(edit);
      setOpen(false);
      reset();
      setSearch({ agent: name, add: undefined });
      toast.success(`${name} copied. Connections aren’t copied. Set them in Connections.`);
    } catch (err) {
      setError(errorText(err));
    } finally {
      setPending(false);
    }
  };

  const firstLines = preview.data?.text.split('\n').slice(0, 6).join('\n');

  return (
    <>
      <Button variant="secondary" className={listButton} onClick={() => setOpen(true)}>
        <Copy size={16} strokeWidth={1.5} aria-hidden="true" />
        Copy from another factory
      </Button>
      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (!next) reset();
          setOpen(next);
        }}
      >
        <DialogContent
          title="Copy an agent from another factory"
          description="Its role, model, prompt and skills come along. Connections don’t."
          className="max-w-[560px]"
        >
          <div className="flex flex-col gap-4">
            <Field label="Factory">
              <NativeSelect
                value={factory}
                onChange={(e) => {
                  setFactory(e.target.value);
                  setAgent('');
                }}
              >
                <option value="">Choose a factory…</option>
                {others.map((f) => (
                  <option key={f.id} value={f.id}>
                    {f.name}
                  </option>
                ))}
              </NativeSelect>
            </Field>
            {factory !== '' ? (
              <Field label="Agent" help={source.isPending ? 'Loading its agents…' : undefined}>
                <NativeSelect value={agent} onChange={(e) => setAgent(e.target.value)} disabled={!model}>
                  <option value="">Choose an agent…</option>
                  {sourceAgents.map((a) => (
                    <option key={a.name} value={a.name}>
                      {a.name}
                    </option>
                  ))}
                </NativeSelect>
              </Field>
            ) : null}
            {picked ? (
              <div className="flex flex-col gap-2 rounded-control border border-line bg-surface p-3 text-sm">
                <p className="m-0">
                  <span className="font-medium">{picked.description || 'No role yet'}</span>
                  <span className="text-muted"> · {picked.model ?? 'the factory’s default model'}</span>
                </p>
                {firstLines ? (
                  <pre className="m-0 max-h-40 overflow-auto font-mono text-[13px] leading-[1.6] whitespace-pre-wrap text-ink">
                    {firstLines}
                  </pre>
                ) : null}
              </div>
            ) : null}
            {error ? (
              <p role="alert" className="m-0 text-[13px] text-rework-ink">
                {error}
              </p>
            ) : null}
            <div className="flex flex-wrap justify-end gap-2">
              <DialogClose asChild>
                <Button variant="secondary" size="lg" className="md:h-10 md:text-sm">
                  Cancel
                </Button>
              </DialogClose>
              <Button
                variant="primary"
                size="lg"
                className="md:h-10 md:text-sm"
                disabled={!picked || pending}
                onClick={() => void copy()}
              >
                {pending ? 'Copying…' : 'Copy'}
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}

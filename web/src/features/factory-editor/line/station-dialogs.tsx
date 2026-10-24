// The Line section's small dialogs (D §10.6): a value asked for by name (a new agent, an output
// contract), a number (parallel agents, a buffer), a check to add, and the lanes a routed station
// sends work by. Each validates before it saves and says what is wrong at the field.
import { Plus, X } from 'lucide-react';
import { type FormEvent, type ReactNode, useId, useRef, useState } from 'react';
import { Button } from '../../../ui/button';
import { Dialog, DialogClose, DialogContent } from '../../../ui/dialog';
import { Field, Input, NativeSelect } from '../../../ui/field';

interface Shared {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: ReactNode;
}

function Actions({ submit }: { submit: string }) {
  return (
    <div className="flex flex-wrap justify-end gap-2">
      <DialogClose asChild>
        <Button variant="white" className="h-11 md:h-10">
          Cancel
        </Button>
      </DialogClose>
      <Button type="submit" variant="primary" className="h-11 md:h-10">
        {submit}
      </Button>
    </div>
  );
}

/** One text value: a name (checked by `check`, which returns the problem) or a contract. */
export function TextDialog({
  open,
  onOpenChange,
  title,
  description,
  label,
  help,
  initial = '',
  submit,
  mono,
  check,
  onSave,
}: Shared & {
  label: string;
  help?: string;
  initial?: string;
  submit: string;
  mono?: boolean;
  check: (value: string) => string | undefined;
  onSave: (value: string) => void;
}) {
  const [value, setValue] = useState(initial);
  const [error, setError] = useState<string>();
  const input = useRef<HTMLInputElement>(null);

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    const v = value.trim();
    const problem = check(v);
    if (problem) {
      setError(problem);
      input.current?.focus();
      return;
    }
    onSave(v);
    onOpenChange(false);
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (o) {
          setValue(initial);
          setError(undefined);
        }
        onOpenChange(o);
      }}
    >
      <DialogContent title={title} description={description} initialFocus={input}>
        <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
          <Field label={label} help={help} error={error}>
            <Input
              ref={input}
              value={value}
              onChange={(e) => {
                setValue(e.target.value);
                setError(undefined);
              }}
              autoComplete="off"
              autoCapitalize="off"
              spellCheck={false}
              className={mono ? 'font-mono md:text-sm' : undefined}
            />
          </Field>
          <Actions submit={submit} />
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** A whole number between min and max; empty clears it when `clearable`. */
export function NumberDialog({
  open,
  onOpenChange,
  title,
  description,
  label,
  help,
  initial,
  min,
  max,
  clearable,
  onSave,
}: Shared & {
  label: string;
  help?: string;
  initial: number | undefined;
  min: number;
  max?: number;
  clearable?: boolean;
  onSave: (value: number | undefined) => void;
}) {
  const [value, setValue] = useState(initial?.toString() ?? '');
  const [error, setError] = useState<string>();
  const input = useRef<HTMLInputElement>(null);

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (value.trim() === '' && clearable) {
      onSave(undefined);
      onOpenChange(false);
      return;
    }
    const n = Number(value);
    if (!Number.isInteger(n) || n < min || (max !== undefined && n > max)) {
      setError(max !== undefined ? `A whole number from ${min} to ${max}.` : `A whole number of at least ${min}.`);
      input.current?.focus();
      return;
    }
    onSave(n);
    onOpenChange(false);
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (o) {
          setValue(initial?.toString() ?? '');
          setError(undefined);
        }
        onOpenChange(o);
      }}
    >
      <DialogContent title={title} description={description} initialFocus={input}>
        <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
          <Field label={label} help={help} error={error}>
            <Input
              ref={input}
              value={value}
              inputMode="numeric"
              onChange={(e) => {
                setValue(e.target.value.replace(/[^0-9]/g, ''));
                setError(undefined);
              }}
              autoComplete="off"
              className="w-28 tabular-nums"
            />
          </Field>
          <Actions submit="Save" />
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** A check before the station moves work on: a command, or its output contract when it has one. */
export function AddCheckDialog({
  open,
  onOpenChange,
  station,
  output,
  hasSchemaCheck,
  onAdd,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  station: string;
  output: string | undefined;
  hasSchemaCheck: boolean;
  onAdd: (check: string | { kind: 'schema' }) => void;
}) {
  const id = useId();
  const canSchema = output !== undefined && !hasSchemaCheck;
  const [kind, setKind] = useState<'command' | 'schema'>('command');
  const [command, setCommand] = useState('');
  const [error, setError] = useState<string>();
  const input = useRef<HTMLInputElement>(null);

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (kind === 'schema') onAdd({ kind: 'schema' });
    else {
      const c = command.trim();
      if (!c) {
        setError('Type the command to run, as in pnpm test.');
        input.current?.focus();
        return;
      }
      onAdd(c);
    }
    onOpenChange(false);
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (o) {
          setKind('command');
          setCommand('');
          setError(undefined);
        }
        onOpenChange(o);
      }}
    >
      <DialogContent
        title={`Add a check to ${station}`}
        description="Work moves on only when every check passes. A failed check sends the item back to the agent with the output."
        initialFocus={input}
      >
        <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
          {canSchema ? (
            <fieldset className="m-0 flex flex-col gap-1 border-0 p-0">
              <legend className="mb-1.5 p-0 text-sm font-medium">What to check</legend>
              <label className="flex min-h-11 cursor-pointer items-center gap-2.5 text-sm md:min-h-9">
                <input
                  type="radio"
                  name={`${id}-kind`}
                  checked={kind === 'command'}
                  onChange={() => setKind('command')}
                  className="size-4 accent-ink"
                />
                A command runs in the workspace
              </label>
              <label className="flex min-h-11 cursor-pointer items-center gap-2.5 text-sm md:min-h-9">
                <input
                  type="radio"
                  name={`${id}-kind`}
                  checked={kind === 'schema'}
                  onChange={() => setKind('schema')}
                  className="size-4 accent-ink"
                />
                <span>
                  The output matches the <span className="font-mono text-[13px]">{output}</span> contract
                </span>
              </label>
            </fieldset>
          ) : null}
          {kind === 'command' ? (
            <Field label="Command" help="Runs in the item's workspace without secrets, as in pnpm test." error={error}>
              <Input
                ref={input}
                value={command}
                onChange={(e) => {
                  setCommand(e.target.value);
                  setError(undefined);
                }}
                autoComplete="off"
                autoCapitalize="off"
                spellCheck={false}
                className="font-mono md:text-sm"
              />
            </Field>
          ) : null}
          <Actions submit="Add check" />
        </form>
      </DialogContent>
    </Dialog>
  );
}

interface Lane {
  key: number;
  lane: string;
  agent: string;
}

const laneRule = /^[a-z0-9]+(-[a-z0-9]+)*$/;

/** The lanes a routed station sends work by: lane → agent rows; work types are suggested as lanes. */
export function LanesDialog({
  open,
  onOpenChange,
  station,
  route,
  agents,
  suggestions,
  onSave,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  station: string;
  route: Record<string, string> | undefined;
  agents: string[];
  suggestions: string[];
  onSave: (route: Record<string, string> | null) => void;
}) {
  const id = useId();
  const next = useRef(0);
  const fresh = (): Lane[] =>
    Object.entries(route ?? {}).map(([lane, agent]) => ({ key: next.current++, lane, agent }));
  const [lanes, setLanes] = useState<Lane[]>(fresh);
  const [error, setError] = useState<string>();
  const first = useRef<HTMLInputElement>(null);

  const set = (key: number, patch: Partial<Lane>) => {
    setLanes((ls) => ls.map((l) => (l.key === key ? { ...l, ...patch } : l)));
    setError(undefined);
  };

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    const rows = lanes.filter((l) => l.lane.trim() !== '' || l.agent !== '');
    const names = rows.map((l) => l.lane.trim());
    if (rows.some((l) => !laneRule.test(l.lane.trim()))) {
      setError('A lane is lowercase letters, digits and dashes, as in backend.');
      return;
    }
    if (new Set(names).size !== names.length) {
      setError('Each lane appears once.');
      return;
    }
    if (rows.some((l) => l.agent === '')) {
      setError('Choose the agent each lane goes to.');
      return;
    }
    onSave(rows.length > 0 ? Object.fromEntries(rows.map((l) => [l.lane.trim(), l.agent])) : null);
    onOpenChange(false);
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (o) {
          setLanes(fresh());
          setError(undefined);
        }
        onOpenChange(o);
      }}
    >
      <DialogContent
        title={`Lanes at ${station}`}
        description="The spec names a lane; work in that lane goes to its agent."
        className="max-w-[560px]"
        initialFocus={first}
      >
        <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
          <datalist id={`${id}-lanes`}>
            {suggestions.map((s) => (
              <option key={s} value={s} />
            ))}
          </datalist>
          {lanes.length > 0 ? (
            <ul className="m-0 flex list-none flex-col gap-2 p-0">
              {lanes.map((l, i) => (
                <li key={l.key} className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] items-end gap-2">
                  <Field label={`Lane ${i + 1}`}>
                    <Input
                      ref={i === 0 ? first : undefined}
                      value={l.lane}
                      list={`${id}-lanes`}
                      onChange={(e) => set(l.key, { lane: e.target.value })}
                      autoComplete="off"
                      autoCapitalize="off"
                      spellCheck={false}
                      className="font-mono md:text-sm"
                    />
                  </Field>
                  <Field label={`Agent for lane ${i + 1}`}>
                    <NativeSelect value={l.agent} onChange={(e) => set(l.key, { agent: e.target.value })}>
                      <option value="">Choose an agent</option>
                      {agents.map((a) => (
                        <option key={a} value={a}>
                          {a}
                        </option>
                      ))}
                    </NativeSelect>
                  </Field>
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={`Remove lane ${l.lane || i + 1}`}
                    onClick={() => setLanes((ls) => ls.filter((x) => x.key !== l.key))}
                    className="size-11 md:size-10"
                  >
                    <X size={16} strokeWidth={1.5} aria-hidden="true" />
                  </Button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="m-0 text-sm text-muted">No lanes: the station has one agent.</p>
          )}
          <div>
            <Button
              variant="white"
              size="sm"
              className="h-11 md:h-8"
              onClick={() => setLanes((ls) => [...ls, { key: next.current++, lane: '', agent: '' }])}
            >
              <Plus size={14} strokeWidth={1.5} aria-hidden="true" />
              Add lane
            </Button>
          </div>
          {error ? (
            <p role="alert" className="m-0 text-[13px] text-rework-ink">
              {error}
            </p>
          ) : null}
          <Actions submit="Save lanes" />
        </form>
      </DialogContent>
    </Dialog>
  );
}

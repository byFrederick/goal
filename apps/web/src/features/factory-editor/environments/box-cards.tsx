// The cards of an environment's box: its size, image and setup commands, the command-line tools it
// installs, the variables it gets (plain text or a secret) and the files it writes.
import { X } from 'lucide-react';
import { type ReactNode, useId, useState } from 'react';
import { cn } from '../../../ui/cn';
import { Field } from '../../../ui/field';
import { Select } from '../../../ui/select';
import {
  type BoxSize,
  type EnvSpec,
  removeEntry,
  secretRef,
  setEntry,
  setImage,
  setList,
  setSize,
  sizes,
} from './env-file';
import { AddButton, Card, CommitInput, RemoveButton, rowPad, SecretSelect, secretMark } from './parts';

export interface CardProps {
  spec: EnvSpec;
  change: (edit: (text: string) => string) => void;
  readOnly: boolean;
  /** Opens the New secret sheet for a variable or a file; `done` gets the new secret's name. */
  newSecret: (done: (name: string) => void) => void;
}

const sizeLabels: Record<BoxSize, string> = {
  small: 'Small: 1 CPU, 2 GB',
  medium: 'Medium: 2 CPUs, 4 GB',
  large: 'Large: 4 CPUs, 8 GB',
};

const defaultImage = '__default__';

export function BoxCard({ spec, change, readOnly }: CardProps) {
  const [adding, setAdding] = useState(false);
  const hint = useId();
  return (
    <Card title="The box" hint="What each run starts from">
      <div className={cn('flex flex-col gap-4 py-4', rowPad)}>
        <div className="grid gap-4 md:grid-cols-2">
          <Field label="Size">
            <Select
              value={spec.size ?? 'medium'}
              disabled={readOnly}
              options={sizes.map((s) => ({ value: s, label: sizeLabels[s] }))}
              onValueChange={(v) => change((t) => setSize(t, v === 'medium' ? undefined : (v as BoxSize)))}
            />
          </Field>
          <Field label="Image">
            {spec.image === undefined ? (
              <Select
                value={defaultImage}
                disabled={readOnly}
                options={[
                  { value: defaultImage, label: 'Goal’s box (default)' },
                  { value: '__custom__', label: 'Another image…' },
                ]}
                onValueChange={(v) => {
                  if (v === '__custom__') change((t) => setImage(t, 'ghcr.io/example/box:latest'));
                }}
              />
            ) : (
              <div className="flex items-center gap-2">
                <CommitInput
                  aria-label="Image"
                  value={spec.image}
                  readOnly={readOnly}
                  onCommit={(v) => change((t) => setImage(t, v))}
                />
                {readOnly ? null : (
                  <RemoveButton label="Use Goal’s box" onClick={() => change((t) => setImage(t, undefined))} />
                )}
              </div>
            )}
          </Field>
        </div>
        <div className="flex flex-col gap-2">
          <span className="text-body font-control">Setup commands</span>
          {spec.setup.map((cmd, i) => (
            <div
              // biome-ignore lint/suspicious/noArrayIndexKey: a command is named by its place; its text changes as it is edited
              key={i}
              className="flex items-center gap-2"
            >
              <CommitInput
                aria-label={`Setup command ${i + 1}`}
                value={cmd}
                readOnly={readOnly}
                onCommit={(v) =>
                  change((t) =>
                    setList(
                      t,
                      'setup',
                      v.trim() === ''
                        ? spec.setup.filter((_, j) => j !== i)
                        : spec.setup.map((c, j) => (j === i ? v : c)),
                    ),
                  )
                }
              />
              {readOnly ? null : (
                <RemoveButton
                  label={`Remove setup command ${i + 1}`}
                  onClick={() =>
                    change((t) =>
                      setList(
                        t,
                        'setup',
                        spec.setup.filter((_, j) => j !== i),
                      ),
                    )
                  }
                />
              )}
            </div>
          ))}
          {adding ? (
            <CommitInput
              aria-label="New setup command"
              value=""
              autoFocus
              placeholder="pnpm install --frozen-lockfile"
              onCommit={(v) => {
                setAdding(false);
                if (v.trim() !== '') change((t) => setList(t, 'setup', [...spec.setup, v.trim()]));
              }}
            />
          ) : null}
          {readOnly ? null : <AddButton onClick={() => setAdding(true)}>Add a command</AddButton>}
          <p id={hint} className="m-0 text-secondary text-muted">
            Run in order after the clone and the tools, in at most 30 minutes.
          </p>
        </div>
      </div>
    </Card>
  );
}

export function ToolsCard({ spec, change, readOnly }: CardProps) {
  const [adding, setAdding] = useState(false);
  return (
    <Card title="Command-line tools" hint="Installed with mise, after the repository’s own mise.toml">
      <div className={cn('flex flex-wrap items-center gap-2 py-4', rowPad)}>
        <ul className="m-0 flex list-none flex-wrap gap-2 p-0">
          {spec.tools.map((tool) => (
            <li
              key={tool}
              className="flex items-center gap-1 rounded-control border border-rule bg-card pl-2.5 font-mono text-secondary"
            >
              {tool}
              {readOnly ? (
                <span className="pr-2.5" />
              ) : (
                <button
                  type="button"
                  aria-label={`Remove ${tool}`}
                  onClick={() =>
                    change((t) =>
                      setList(
                        t,
                        'tools',
                        spec.tools.filter((x) => x !== tool),
                      ),
                    )
                  }
                  className="press relative inline-flex size-6 cursor-pointer items-center justify-center rounded-control text-muted hover:text-ink max-md:size-8"
                >
                  <X size={12} strokeWidth={1.75} aria-hidden="true" />
                </button>
              )}
            </li>
          ))}
        </ul>
        {adding ? (
          <CommitInput
            aria-label="New tool"
            value=""
            autoFocus
            placeholder="go@1.26"
            className="w-44"
            onCommit={(v) => {
              setAdding(false);
              const tool = v.trim();
              if (tool !== '' && !spec.tools.includes(tool)) change((t) => setList(t, 'tools', [...spec.tools, tool]));
            }}
          />
        ) : readOnly ? null : (
          <AddButton onClick={() => setAdding(true)}>Add</AddButton>
        )}
        {spec.tools.length === 0 && !adding ? <span className="text-secondary text-muted">None yet.</span> : null}
      </div>
    </Card>
  );
}

const nameOk = /^[A-Za-z_][A-Za-z0-9_]*$/;
const pathOk = /^(\/|~\/).+/;

/** A row of variables or files: the name, then plain text or a secret. */
function EntryRows({ kind, spec, change, readOnly, newSecret }: CardProps & { kind: 'env' | 'files' }) {
  const entries = Object.entries(kind === 'env' ? spec.env : spec.files);
  const [pending, setPending] = useState(false);
  const files = kind === 'files';
  const valid = files ? pathOk : nameOk;
  const problem = files
    ? 'Use an absolute path or one that starts with ~/.'
    : 'Letters, digits and underscores, not starting with a digit.';
  const [bad, setBad] = useState<string | null>(null);
  const errorId = useId();
  const set = (name: string, value: string, rename?: string) => change((t) => setEntry(t, kind, name, value, rename));

  return (
    <div className={cn('flex flex-col gap-3 py-4', rowPad)}>
      {entries.map(([name, value]) => {
        const secret = secretRef(value);
        const useSecret = files || secret !== undefined;
        return (
          <div
            key={name}
            className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-2 gap-y-2 md:grid-cols-[minmax(0,5fr)_auto_minmax(0,7fr)_auto]"
          >
            <CommitInput
              aria-label={files ? `Path of ${name}` : `Name of ${name}`}
              value={name}
              readOnly={readOnly}
              invalid={bad === name}
              describedBy={bad === name ? errorId : undefined}
              onCommit={(v) => {
                if (!valid.test(v)) return setBad(name);
                setBad(null);
                set(v, value, name);
              }}
            />
            <span aria-hidden="true" className="hidden text-muted md:block">
              =
            </span>
            <div className="order-last col-span-2 min-w-0 md:order-none md:col-span-1">
              {useSecret ? (
                <SecretSelect
                  label={`Secret for ${name}`}
                  value={secret}
                  disabled={readOnly}
                  placeholder="Pick a secret"
                  withNew={() => newSecret((n) => set(name, `{{${n}}}`))}
                  onChange={(n) => set(name, `{{${n}}}`)}
                />
              ) : (
                <div className="flex items-center gap-2">
                  <CommitInput
                    aria-label={`Value of ${name}`}
                    value={value}
                    readOnly={readOnly}
                    onCommit={(v) => set(name, v)}
                  />
                  {readOnly ? null : (
                    <button
                      type="button"
                      onClick={() => set(name, '')}
                      className="press inline-flex min-h-11 shrink-0 cursor-pointer items-center gap-1.5 whitespace-nowrap rounded-control px-2 text-secondary text-ink hover:text-link md:min-h-8"
                    >
                      {secretMark}Use a secret
                    </button>
                  )}
                </div>
              )}
            </div>
            {readOnly ? null : (
              <RemoveButton label={`Remove ${name}`} onClick={() => change((t) => removeEntry(t, kind, name))} />
            )}
          </div>
        );
      })}
      {bad ? (
        <p id={errorId} role="alert" className="m-0 text-secondary text-rework-ink">
          {problem}
        </p>
      ) : null}
      {pending ? (
        <CommitInput
          aria-label={files ? 'New file path' : 'New variable name'}
          value=""
          autoFocus
          placeholder={files ? '~/.kube/config' : 'SENTRY_ORG'}
          onCommit={(v) => {
            setPending(false);
            if (v === '') return;
            if (!valid.test(v)) return setBad(v);
            setBad(null);
            set(v, '');
          }}
        />
      ) : null}
      {readOnly ? null : (
        <AddButton onClick={() => setPending(true)}>{files ? 'Add a file' : 'Add a variable'}</AddButton>
      )}
    </div>
  );
}

export function VariablesCard(props: CardProps & { checks: ReactNode }) {
  const { checks, ...rest } = props;
  return (
    <Card title="Variables" hint="What CLIs and API calls read: plain text or a secret">
      <EntryRows kind="env" {...rest} />
      {checks}
    </Card>
  );
}

export function FilesCard(props: CardProps) {
  const none = Object.keys(props.spec.files).length === 0;
  return (
    <Card title="Files" {...(none ? {} : { hint: 'Written before setup, mode 0600' })}>
      {none ? (
        <p className={cn('m-0 py-4 text-body text-muted', rowPad)}>
          None. A secret can be written to a path, such as a kubeconfig or an SSH key.
        </p>
      ) : null}
      <EntryRows kind="files" {...props} />
    </Card>
  );
}

// What the Environments section and Add a tool share: a card with its title and hint, an input that
// writes when it is left (typing never rewrites the file), the remove button, the secret picker, the
// Allow, Ask first and Off control, the mark that starts a server's row, and the test hook.
import { useMutation, useQuery } from '@tanstack/react-query';
import { KeyRound, X } from 'lucide-react';
import { type ReactNode, useEffect, useId, useState } from 'react';
import { ApiError } from '../../../api/client';
import { testEnvironment } from '../../../api/definitions';
import { secretsQuery } from '../../../api/queries';
import type { EnvironmentTestRequest, ServerTest } from '../../../api/types';
import { useOrg } from '../../../shared/org';
import { cn } from '../../../ui/cn';
import { Input } from '../../../ui/field';
import { sectionHeadingClass } from '../../../ui/page-header';
import { SegmentedControl } from '../../../ui/segmented-control';
import { Select, type SelectOption } from '../../../ui/select';
import { useDraftSession } from '../draft/use-draft';
import type { ToolMode } from './env-file';

export const cardClass = 'flex min-w-0 flex-col rounded-card border border-line bg-card';
export const rowPad = 'px-4 md:px-5';

/** A card of the section: its title with a hint beside it, an optional aside, then the body. */
export function Card({
  title,
  hint,
  aside,
  children,
  className,
}: {
  title: string;
  hint?: string | undefined;
  aside?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  const id = useId();
  return (
    <section aria-labelledby={id} className={cn(cardClass, className)}>
      <div
        className={cn(
          'flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 border-b border-line py-4',
          rowPad,
        )}
      >
        <h3 id={id} className={cn(sectionHeadingClass, 'm-0')}>
          {title}
          {hint ? <span className="ml-2 text-secondary font-normal text-muted">{hint}</span> : null}
        </h3>
        {aside}
      </div>
      {children}
    </section>
  );
}

/** A text link-button that adds a row ("Add a command"): 44 px on phones. */
export function AddButton({
  children,
  onClick,
  disabled,
}: {
  children: ReactNode;
  onClick: () => void;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className="press inline-flex min-h-11 w-fit cursor-pointer items-center gap-2 rounded-control text-body text-ink transition-colors duration-(--duration-quick) hover:text-link disabled:cursor-default disabled:text-muted md:min-h-8"
    >
      <span aria-hidden="true" className="text-lg leading-none">
        +
      </span>
      {children}
    </button>
  );
}

export function RemoveButton({ label, onClick, disabled }: { label: string; onClick: () => void; disabled?: boolean }) {
  return (
    <button
      type="button"
      aria-label={label}
      onClick={onClick}
      disabled={disabled}
      className="press relative inline-flex size-8 shrink-0 cursor-pointer items-center justify-center rounded-control text-muted transition-colors duration-(--duration-quick) hover:bg-hover hover:text-ink disabled:cursor-default disabled:opacity-40 max-md:size-11"
    >
      <X size={16} strokeWidth={1.75} aria-hidden="true" />
    </button>
  );
}

/** An input that writes when it is left or on Enter, and shows the file's value again when that changes. */
export function CommitInput({
  value,
  onCommit,
  invalid,
  describedBy,
  className,
  ...rest
}: {
  value: string;
  onCommit: (value: string) => void;
  invalid?: boolean;
  describedBy?: string | undefined;
  className?: string;
  'aria-label': string;
  placeholder?: string;
  readOnly?: boolean;
  autoFocus?: boolean;
}) {
  const [text, setText] = useState(value);
  useEffect(() => setText(value), [value]);
  const commit = () => {
    if (text !== value) onCommit(text);
  };
  return (
    <Input
      {...rest}
      value={text}
      aria-invalid={invalid || undefined}
      aria-describedby={describedBy}
      onChange={(e) => setText(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === 'Enter') commit();
      }}
      autoComplete="off"
      autoCapitalize="off"
      spellCheck={false}
      className={cn('font-mono', className)}
    />
  );
}

/** The organization's secrets as a select's options. */
export function useSecretOptions(extra: readonly string[] = []): { options: SelectOption[]; ready: boolean } {
  const org = useOrg();
  const secrets = useQuery(secretsQuery(org.id));
  const names = new Set((secrets.data ?? []).map((s) => s.name));
  const options = [...(secrets.data ?? []).map((s) => s.name), ...extra.filter((n) => !names.has(n))].map((name) => ({
    value: name,
    label: name,
  }));
  return { options, ready: secrets.data !== undefined };
}

/** Picks one of the organization's secrets; a name the file holds that the organization lacks still shows, so nothing is hidden. */
export function SecretSelect({
  value,
  onChange,
  label,
  placeholder = 'Pick a secret',
  disabled,
  withNew,
  newLabel = 'New secret…',
  className,
}: {
  value: string | undefined;
  onChange: (name: string) => void;
  label: string;
  placeholder?: string;
  disabled?: boolean;
  /** Adds "New secret…" to the list; choosing it calls this. */
  withNew?: (() => void) | undefined;
  newLabel?: string;
  className?: string;
}) {
  const { options } = useSecretOptions(value ? [value] : []);
  const NEW = '__new__';
  return (
    <Select
      aria-label={label}
      value={value ?? ''}
      placeholder={placeholder}
      disabled={disabled}
      className={cn('font-mono', className)}
      options={withNew ? [...options, { value: NEW, label: newLabel }] : options}
      onValueChange={(v) => (v === NEW ? withNew?.() : onChange(v))}
    />
  );
}

export const secretMark = <KeyRound size={13} strokeWidth={1.75} aria-hidden="true" className="shrink-0 text-muted" />;

const modeOptions = [
  { value: 'allow', label: 'Allow' },
  { value: 'ask', label: 'Ask first' },
  { value: 'off', label: 'Off' },
] as const;

/** Allow, Ask first or Off for a tool or a group of tools (MOTION.md, Tabs and segmented controls). */
export function ModeControl({
  label,
  value,
  onChange,
  readOnly,
}: {
  label: string;
  value: ToolMode | undefined;
  onChange: (mode: ToolMode) => void;
  readOnly?: boolean;
}) {
  return (
    <SegmentedControl<ToolMode>
      label={label}
      options={modeOptions}
      value={value}
      onChange={onChange}
      {...(readOnly ? { readOnly } : {})}
      className="shrink-0"
    />
  );
}

/** Two letters in a bordered square: the server's mark. */
export function Initials({ name }: { name: string }) {
  return (
    <span
      aria-hidden="true"
      className="flex size-9 shrink-0 items-center justify-center rounded-control border border-line bg-card text-secondary font-control text-ink"
    >
      {name.slice(0, 2).replace(/^./, (c) => c.toUpperCase())}
    </span>
  );
}

/** How a test failed, in words: goald's reason, or that goald could not be reached. */
export const testFailure = (err: unknown) =>
  err instanceof ApiError ? (err.detail ?? err.title) : 'goald can’t be reached';

/**
 * Tests an environment's MCP servers (or a template) against the draft: what is typed is saved first, so
 * the test reads what the file says. The answers are kept by server name until the next test.
 */
export function useEnvironmentTest(environment: string) {
  const session = useDraftSession();
  const [results, setResults] = useState<Record<string, ServerTest>>({});
  const [tested, setTested] = useState<Date | null>(null);
  const test = useMutation({
    mutationFn: async (request: EnvironmentTestRequest) => {
      await session.flush();
      return testEnvironment(session.org, session.factory, environment, request);
    },
    gcTime: 0,
    onSuccess: (answer) => {
      setResults((cur) => ({ ...cur, ...Object.fromEntries(answer.servers.map((s) => [s.server, s])) }));
      setTested(new Date());
    },
  });
  return { results, tested, test, run: (request: EnvironmentTestRequest = {}) => test.mutate(request) };
}

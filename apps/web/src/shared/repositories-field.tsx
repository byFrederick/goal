// New item's Repositories field (ADR 0041 decision 4): the repositories the item works on, as chips with a
// Remove button, and Add, which opens a combobox. It lists the factory's single-repository entries as
// owner/name and takes a typed owner/name under an owner-wide entry (https://host/owner/*). goald checks
// every value; this file only offers them and stops at ten.
import { Plus, X } from 'lucide-react';
import { type KeyboardEvent, useId, useState } from 'react';
import { Button } from '../ui/button';
import { cn } from '../ui/cn';
import { InfoTip } from '../ui/info-tip';
import { Leaving, useLeaving } from '../ui/leaving';
import { Popover, PopoverContent, PopoverTrigger } from '../ui/popover';
import type { RepositoryEntry } from './definition/model';

/** The most repositories an item takes (CreateItemRequest.repositories). */
export const MAX_REPOSITORIES = 10;

/** The entry's path after its host, without a trailing .git or /*; empty for a URL that is not one. */
function pathOf(url: string): string {
  try {
    return new URL(url.trim()).pathname
      .replace(/^\/+|\/+$/g, '')
      .replace(/\/\*$/, '')
      .replace(/\.git$/, '');
  } catch {
    return '';
  }
}

/** An owner's every repository (…/acme/*): a person types the repository under it. */
const isOwnerWide = (e: RepositoryEntry) => /\/\*$/.test(e.url.trim());

/** What a factory's entries offer a person filing an item. */
export interface RepositoryChoices {
  /** The single-repository entries, as owner/name. */
  options: string[];
  /** The owner-wide entries' owners (acme for …/acme/*): a typed owner/name under one is offered. */
  owners: string[];
  /** The default entry's repository, which an item starts with; absent without a default entry. */
  initial: string | undefined;
}

export function choicesOf(entries: readonly RepositoryEntry[]): RepositoryChoices {
  const options: string[] = [];
  const owners: string[] = [];
  for (const e of entries) {
    const path = pathOf(e.url);
    if (!path) continue;
    if (isOwnerWide(e)) owners.push(path.toLowerCase());
    else if (!options.includes(path)) options.push(path);
  }
  const fallback = entries.find((e) => e.default && !isOwnerWide(e));
  return { options, owners, initial: fallback ? pathOf(fallback.url) || undefined : undefined };
}

/** Whether a typed owner/name is a repository an owner-wide entry covers. */
function typedFits(value: string, owners: readonly string[]): boolean {
  const parts = value.split('/');
  if (parts.length < 2 || parts.some((p) => p === '')) return false;
  return owners.some((o) => value.toLowerCase().startsWith(`${o}/`) && parts.length === o.split('/').length + 1);
}

const same = (a: string, b: string) => a.toLowerCase() === b.toLowerCase();

export function RepositoriesField({
  choices,
  value,
  onChange,
  factory,
  error,
}: {
  choices: RepositoryChoices;
  value: string[];
  onChange: (next: string[]) => void;
  /** The factory's title, in the tooltip. */
  factory: string;
  /** goald's refusal of a value, as it said it. */
  error?: string | undefined;
}) {
  const full = value.length >= MAX_REPOSITORIES;
  return (
    <fieldset className="m-0 flex min-w-0 flex-col gap-1 border-0 p-0">
      <legend className="mb-1 flex items-center gap-1 p-0 text-sm text-muted">
        Repositories
        <InfoTip about="Repositories" className="max-md:size-11">
          {`Agents start with these, and open any other repository ${factory} covers as they need them.`}
        </InfoTip>
      </legend>
      <div
        aria-invalid={error ? true : undefined}
        className={cn('flex min-w-0 flex-wrap items-center gap-2', error && 'motion-safe:animate-error-shake')}
      >
        {value.map((repo) => (
          <RepoChip key={repo} repo={repo} onRemove={() => onChange(value.filter((r) => r !== repo))} />
        ))}
        {full ? (
          <span className="text-sm text-muted">Up to {MAX_REPOSITORIES}</span>
        ) : (
          <AddRepository choices={choices} value={value} onAdd={(repo) => onChange([...value, repo])} />
        )}
      </div>
      {error ? (
        <p role="alert" className="m-0 animate-fade-in text-meta text-rework-ink">
          {error}
        </p>
      ) : null}
    </fieldset>
  );
}

function RepoChip({ repo, onRemove }: { repo: string; onRemove: () => void }) {
  const { leaving, leave } = useLeaving(onRemove);
  return (
    <Leaving leaving={leaving} className="min-w-0 max-w-full">
      <span
        className={cn(
          'inline-flex h-7 max-w-full items-center gap-1 rounded-chip border border-line bg-panel pr-1 pl-3 max-md:h-11 text-secondary text-ink',
          // A chip that is added fades in and rises 4px (MOTION.md, A new item appears in a list or column).
          'motion-safe:animate-item-in motion-reduce:animate-fade-in',
        )}
      >
        <span title={repo} className="min-w-0 truncate font-mono">
          {repo}
        </span>
        <button
          type="button"
          aria-label={`Remove ${repo}`}
          onClick={leave}
          className="press inline-flex size-6 flex-none max-md:size-11 items-center justify-center rounded-chip text-muted transition-colors duration-(--duration-quick) hover:text-ink motion-reduce:transition-none"
        >
          <X size={14} strokeWidth={1.5} aria-hidden="true" />
        </button>
      </span>
    </Leaving>
  );
}

/** Add: a popover with a text box and the choices left, picked with the mouse or Arrow keys and Enter. */
function AddRepository({
  choices,
  value,
  onAdd,
}: {
  choices: RepositoryChoices;
  value: string[];
  onAdd: (repo: string) => void;
}) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [text, setText] = useState('');
  const [active, setActive] = useState(0);
  const query = text.trim();
  const left = choices.options.filter(
    (o) => !value.some((v) => same(v, o)) && o.toLowerCase().includes(query.toLowerCase()),
  );
  const typed =
    query !== '' &&
    !left.some((o) => same(o, query)) &&
    !value.some((v) => same(v, query)) &&
    typedFits(query, choices.owners);
  const shown = typed ? [...left, query] : left;
  const at = Math.min(active, Math.max(0, shown.length - 1));

  const take = (repo: string) => {
    onAdd(repo);
    setText('');
    setActive(0);
    setOpen(false);
  };
  const onKey = (e: KeyboardEvent) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (shown.length > 0) setActive((at + (e.key === 'ArrowDown' ? 1 : -1) + shown.length) % shown.length);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      const repo = shown[at];
      if (repo) take(repo);
    }
  };
  const none =
    choices.options.length === 0 && choices.owners.length > 0
      ? 'Type owner/name.'
      : left.length === 0 && !typed
        ? choices.owners.length > 0
          ? 'No match. Type owner/name.'
          : 'No more repositories.'
        : null;

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setText('');
      }}
    >
      <PopoverTrigger asChild>
        <Button variant="white" size="sm">
          <Plus size={14} strokeWidth={1.5} aria-hidden="true" />
          Add
        </Button>
      </PopoverTrigger>
      <PopoverContent side="bottom" align="start" className="w-72 gap-1">
        <input
          // biome-ignore lint/a11y/noAutofocus: the popover opens to type into it
          autoFocus
          role="combobox"
          aria-label="Repository"
          aria-expanded="true"
          aria-controls={`${id}-list`}
          aria-activedescendant={shown.length > 0 ? `${id}-${at}` : undefined}
          aria-autocomplete="list"
          value={text}
          onChange={(e) => {
            setText(e.target.value);
            setActive(0);
          }}
          onKeyDown={onKey}
          placeholder={choices.owners.length > 0 ? 'owner/name' : 'Search'}
          className="h-11 w-full min-w-0 rounded-control border border-rule bg-card px-3 text-base text-ink placeholder:text-muted md:h-8 md:text-body"
        />
        <div id={`${id}-list`} role="listbox" aria-label="Repositories" className="flex flex-col">
          {shown.map((repo, i) => (
            // The text box takes the keys (Arrow keys, Enter); an option is only for the pointer.
            // Moving the highlight with the keyboard does not animate.
            // biome-ignore lint/a11y/useKeyWithClickEvents: the combobox's input handles the keyboard
            <div
              key={repo}
              title={repo}
              id={`${id}-${i}`}
              role="option"
              aria-selected={i === at}
              tabIndex={-1}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => take(repo)}
              className={cn(
                'min-w-0 cursor-pointer truncate rounded-control px-3 py-2 font-mono text-secondary text-ink',
                i === at && 'bg-hover',
              )}
            >
              {repo}
              {typed && i === shown.length - 1 ? <span className="font-sans text-muted"> (typed)</span> : null}
            </div>
          ))}
        </div>
        {none ? <p className="m-0 px-3 py-2 text-sm text-muted">{none}</p> : null}
      </PopoverContent>
    </Popover>
  );
}

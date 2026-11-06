// The factory's repositories (ADR 0041 decision 9): runtime.repositories, each with a name, an https URL, its
// access (read or write), the secret that opens it as {{secret}} and whether items without a repository use it.
// A factory with none shows "No code. Agents work through their tools."; a definition on the runtime.repository
// shorthand keeps RepositoryField.
import { Plus, X } from 'lucide-react';
import { useId } from 'react';
import type { BasicsSection, RepositoryEntry } from '../../../shared/definition/model';
import { Button } from '../../../ui/button';
import { Checkbox } from '../../../ui/checkbox';
import { cn } from '../../../ui/cn';
import { Field, Input } from '../../../ui/field';
import { InfoTip } from '../../../ui/info-tip';
import { SegmentedControl } from '../../../ui/segmented-control';
import { setBasics } from '../draft/ops';
import { useDraftSession, useViewOnly } from '../draft/use-draft';
import { secretRef } from '../environments/env-file';
import { NewSecretSheetFor } from '../environments/new-secret';
import { SecretSelect } from '../environments/parts';
import { ProblemFixes } from '../frame/problem-fixes';
import { EditedMark } from '../frame/section-shell';
import { useProblemText } from '../frame/use-editor';

const access = [
  { value: 'read', label: 'Read only' },
  { value: 'write', label: 'Read and write' },
] as const;

/** The https URL's owner pattern (https://github.com/acme/*): every repository of an owner, never the default. */
export const isOwnerPattern = (url: string) => /\/\*$/.test(url.trim());

/** A name no entry has: repo, repo-2, repo-3… */
export function freshName(entries: readonly RepositoryEntry[]): string {
  const taken = new Set(entries.map((e) => e.name));
  if (!taken.has('repo')) return 'repo';
  for (let n = 2; ; n += 1) if (!taken.has(`repo-${n}`)) return `repo-${n}`;
}

/** The entries with `patch` applied to entry `index`; a default moves to that entry alone. */
export function patchEntry(entries: readonly RepositoryEntry[], index: number, patch: Partial<RepositoryEntry>) {
  return entries.map((e, i) => {
    if (i === index) {
      const next = { ...e, ...patch };
      if (next.token === '' || next.token === undefined) delete next.token;
      if (next.default !== true) delete next.default;
      return next;
    }
    if (patch.default === true && e.default) {
      const { default: _d, ...rest } = e;
      return rest;
    }
    return e;
  });
}

export function RepositoriesField({ basics, shorthand }: { basics: BasicsSection; shorthand: boolean }) {
  const session = useDraftSession();
  const viewOnly = useViewOnly();
  const entries = basics.repositories ?? [];
  const editEntries = (next: RepositoryEntry[], coalesce?: string) =>
    session.edit(setBasics(basics, 'repositories', next.length > 0 ? next : undefined), coalesce ? { coalesce } : {});

  return (
    <div className="col-span-full flex min-w-0 flex-col gap-3">
      <div className="flex min-h-6 flex-wrap items-center justify-between gap-x-3 gap-y-1">
        <span className="flex items-center gap-1 text-sm font-medium">
          Repositories
          <InfoTip about="Repositories">
            The repositories each run may open. Read only repositories are cloned for reading; agents whose code is
            write push, open pull requests and merge in the ones that write. A token is needed to write.
          </InfoTip>
        </span>
        <EditedMark section="basics" pointer="/basics/repositories" label="the repositories" />
      </div>
      {entries.length === 0 ? (
        shorthand ? null : (
          <p className="m-0 text-sm text-muted">No code. Agents work through their tools.</p>
        )
      ) : (
        <ul className="m-0 flex list-none flex-col gap-3 p-0">
          {entries.map((_entry, index) => (
            <EntryRow
              // Rows are matched by position: a name is edited in place.
              // biome-ignore lint/suspicious/noArrayIndexKey: the name is the field being typed into
              key={index}
              entries={entries}
              index={index}
              readOnly={viewOnly}
              onChange={editEntries}
            />
          ))}
        </ul>
      )}
      {viewOnly ? null : (
        <Button
          variant="white"
          size="sm"
          className="self-start"
          onClick={() => editEntries([...entries, { name: freshName(entries), url: '', access: 'read' }])}
        >
          <Plus size={14} strokeWidth={1.5} aria-hidden="true" />
          Add a repository
        </Button>
      )}
    </div>
  );
}

function EntryRow({
  entries,
  index,
  readOnly,
  onChange,
}: {
  entries: RepositoryEntry[];
  index: number;
  readOnly: boolean;
  onChange: (next: RepositoryEntry[], coalesce?: string) => void;
}) {
  const entry = entries[index] as RepositoryEntry;
  const at = `/basics/repositories/${index}`;
  const urlProblem = useProblemText(`${at}/url`);
  const nameProblem = useProblemText(`${at}/name`);
  const newSecret = NewSecretSheetFor({ name: 'github-token' });
  const help = useId();
  const token = secretRef(entry.token ?? '') ?? (entry.token || undefined);
  const owner = isOwnerPattern(entry.url);
  const only = entries.length === 1;
  const patch = (p: Partial<RepositoryEntry>, coalesce?: string) => onChange(patchEntry(entries, index, p), coalesce);
  const pick = (name: string) => patch({ token: `{{${name}}}` });

  return (
    <li>
      <fieldset
        aria-label={entry.name ? `Repository ${entry.name}` : 'New repository'}
        className={cn(
          'flex min-w-0 flex-col gap-3 rounded-control border border-line bg-card p-3',
          // A row added to the draft fades in and rises 4px (MOTION.md, A new item appears in a list or column).
          'motion-safe:animate-item-in motion-reduce:animate-fade-in',
        )}
      >
        <div className="grid grid-cols-1 gap-2 @[640px]:grid-cols-[180px_minmax(0,1fr)]">
          <Field label="Name" error={nameProblem}>
            <Input
              value={entry.name}
              readOnly={readOnly}
              autoComplete="off"
              spellCheck={false}
              className="font-mono"
              onChange={(e) => patch({ name: e.target.value }, `${at}/name`)}
            />
          </Field>
          <Field label="URL" error={urlProblem}>
            <Input
              type="url"
              inputMode="url"
              value={entry.url}
              readOnly={readOnly}
              aria-describedby={help}
              placeholder="https://github.com/acme/app"
              autoComplete="off"
              spellCheck={false}
              onChange={(e) => patch({ url: e.target.value }, `${at}/url`)}
            />
          </Field>
        </div>
        <div className="flex flex-wrap items-end gap-x-4 gap-y-3">
          <div className="flex min-w-0 flex-col gap-2">
            <span aria-hidden="true" className="text-sm font-medium">
              Access
            </span>
            <SegmentedControl
              label={`Access of ${entry.name}`}
              options={access}
              value={entry.access}
              readOnly={readOnly}
              onChange={(a) => patch({ access: a })}
            />
          </div>
          <div className="flex min-w-0 flex-1 basis-[220px] flex-col gap-2">
            <span className="text-sm font-medium">Token</span>
            <SecretSelect
              label={`Token of ${entry.name}`}
              value={token}
              onChange={pick}
              disabled={readOnly}
              placeholder={entry.access === 'write' ? 'Pick a secret' : 'Public, no token'}
              withNew={() => newSecret.open(pick)}
              className="w-full"
            />
          </div>
          <span className="flex min-h-8 items-center gap-1">
            <Checkbox
              label="Default"
              checked={entry.default === true || (only && !owner)}
              disabled={readOnly || owner || only}
              onCheckedChange={(on) => patch({ default: on })}
            />
            <InfoTip about="Default">
              The repository an item uses when it names none. The only repository is the default; an owner’s every
              repository (ending in /*) cannot be.
            </InfoTip>
          </span>
          {readOnly ? null : (
            <Button
              variant="ghost"
              size="sm"
              className="ml-auto"
              aria-label={`Remove repository ${entry.name}`}
              onClick={() =>
                onChange(
                  entries.filter((_, i) => i !== index),
                  undefined,
                )
              }
            >
              <X size={14} strokeWidth={1.5} aria-hidden="true" />
              Remove
            </Button>
          )}
        </div>
        {newSecret.sheet}
        <p id={help} className="sr-only">
          An https clone URL without credentials, or an owner’s every repository ending in /*.
        </p>
        <ProblemFixes pointer={`${at}/token`} />
        <ProblemFixes pointer={`${at}/access`} />
      </fieldset>
    </li>
  );
}

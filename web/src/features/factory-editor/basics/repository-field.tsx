// The repository (D §10.5): an https URL, or none for a factory that doesn't change code. How it
// signs in is read-only until credentials arrive: the draft's `env:` reference is named, never
// rewritten (D §14.4).
import { useId, useRef } from 'react';
import type { BasicsSection, Repository } from '../../../shared/definition/model';
import { Field, Input } from '../../../ui/field';
import { setBasics } from '../draft/ops';
import { useDraft, useDraftSession, useViewOnly } from '../draft/use-draft';
import { EditedMark } from '../frame/section-shell';
import { useProblemText } from '../frame/use-editor';

function signsIn(repository: Repository | null | undefined): string {
  const token = repository?.token;
  if (token?.startsWith('env:')) {
    return `Signs in with ${token}, a platform secret of this install. Choosing a credential arrives with credentials.`;
  }
  return 'How it signs in is set in YAML. Choosing a credential arrives with credentials.';
}

export function RepositoryField({ basics }: { basics: BasicsSection }) {
  const session = useDraftSession();
  const viewOnly = useViewOnly();
  const id = useId();
  const saved = useDraft((s) => s.sections.basics.server.repository);
  // The repository a "None" replaced, put back when it is unchecked.
  const before = useRef<Repository | null | undefined>(null);
  const repository = basics.repository;
  const none = repository === null;
  const problem = useProblemText('/basics/repository');

  const setUrl = (url: string) =>
    session.edit(setBasics(basics, 'repository', { ...repository, url }), { coalesce: '/basics/repository/url' });

  const setNone = (on: boolean) => {
    if (on) {
      before.current = repository;
      session.edit(setBasics(basics, 'repository', null));
      return;
    }
    const back = before.current ?? (saved ? saved : { url: '' });
    session.edit(setBasics(basics, 'repository', back));
  };

  return (
    <Field
      label="Repository"
      aside={<EditedMark section="basics" pointer="/basics/repository" label="the repository" />}
      error={problem}
      help={none ? 'Agents work without a repository: no checkout, no pull requests.' : signsIn(repository)}
    >
      <Input
        type="url"
        inputMode="url"
        value={repository?.url ?? ''}
        disabled={none}
        onChange={(e) => setUrl(e.target.value)}
        onBlur={() => void session.flush()}
        placeholder="https://github.com/acme/goal"
        autoComplete="off"
        spellCheck={false}
        className="disabled:cursor-not-allowed disabled:bg-control disabled:text-muted"
      />
      <label
        htmlFor={`${id}-none`}
        className="inline-flex min-h-11 cursor-pointer items-center gap-2 text-sm has-[:disabled]:cursor-default md:min-h-8"
      >
        <input
          id={`${id}-none`}
          type="checkbox"
          checked={none}
          disabled={viewOnly}
          onChange={(e) => setNone(e.target.checked)}
          className="size-4 accent-ink"
        />
        None, this factory doesn’t change code
      </label>
    </Field>
  );
}

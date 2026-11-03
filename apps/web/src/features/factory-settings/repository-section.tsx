// Settings › Repository: the repository the runs clone (runtime.repository: its URL and the token secret
// git signs in with, docs/design/screens/build/factory-settings.jpg). It shows the URL with a "Token works"
// chip (the token tested for clone and push), the secret's name and Change, which opens the Definition
// editor's field in place; that field writes through patchRepository. With no repository set the field
// shows directly. Base branch has no field in the factory definition yet (ADR 0016, decision 4), so it
// stays "Not available yet".
import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { repositoryTestQuery } from '../../api/queries';
import { useOrg, useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { Chip } from '../../ui/chip';
import { Field, Input } from '../../ui/field';
import { NotAvailable } from '../../ui/not-available';
import { RepositoryField } from '../factory-editor/basics/repository-field';
import { useSection, useViewOnly } from '../factory-editor/draft/use-draft';
import { secretRef } from '../factory-editor/environments/env-file';
import { checkText } from '../onboarding/code-step';
import { SettingsGroup } from './settings-group';

/** What the token can do, from the host: Token works, or the reason it fails; quiet while it is not known. */
function TokenChip({ url, secret }: { url: string; secret: string }) {
  const org = useOrg();
  const allowed = useOrgHolds('factory:create');
  const test = useQuery({ ...repositoryTestQuery(org.id, url, secret), enabled: allowed });
  if (!allowed) return null;
  if (test.isPending) return <Chip tone="quiet">Testing the token</Chip>;
  if (test.isError) return <Chip tone="quiet">Token not tested</Chip>;
  const found = checkText(url.replace(/^https?:\/\/[^/]+\//, '').replace(/\.git$/, ''), test.data);
  return found.ok ? (
    <Chip tone="ok" dot>
      Token works
    </Chip>
  ) : (
    <>
      <Chip tone="rework" dot>
        Token fails
      </Chip>
      <span className="text-secondary text-rework-ink">{found.text}</span>
    </>
  );
}

export function RepositorySection() {
  const basics = useSection('basics');
  const viewOnly = useViewOnly();
  const [editing, setEditing] = useState(false);
  const repository = basics.repository ?? undefined;
  const set = Boolean(repository?.url || repository?.token);
  const secret = secretRef(repository?.token ?? '');
  return (
    <SettingsGroup
      id="repository"
      title="Repository"
      footer={
        <Field label="Base branch" help="Runs open their pull requests against this branch.">
          <NotAvailable label="Base branch">
            <Input readOnly aria-label="Base branch" placeholder="Not available yet" value="" />
          </NotAvailable>
        </Field>
      }
    >
      {editing || !set ? (
        <RepositoryField basics={basics} />
      ) : (
        <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2">
          <span className="min-w-0 flex-1 truncate text-body" title={repository?.url || 'No repository URL'}>
            {repository?.url?.replace(/^https:\/\//, '') || 'No repository URL'}
          </span>
          {repository?.url && secret ? (
            <TokenChip url={repository.url} secret={secret} />
          ) : (
            <Chip tone="quiet">Public, no token</Chip>
          )}
          {secret ? <span className="font-mono text-meta text-muted">{secret}</span> : null}
          {viewOnly ? null : (
            <Button variant="ghost" onClick={() => setEditing(true)}>
              Change
            </Button>
          )}
        </div>
      )}
    </SettingsGroup>
  );
}

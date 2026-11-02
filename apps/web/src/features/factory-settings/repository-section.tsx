// Settings › Repository: the repository the runs clone (basics.repository: its URL and the connection git
// signs in with). It shows the URL with a "Connected with <connection>" chip and Change, which opens the
// Definition editor's field in place; that field writes through patchRepository. With no repository set the
// field shows directly. Base branch has no field in the factory definition yet (ADR 0016, decision 4), so it
// stays "Not available yet".
import { useState } from 'react';
import { Button } from '../../ui/button';
import { Chip } from '../../ui/chip';
import { Field, Input } from '../../ui/field';
import { NotAvailable } from '../../ui/not-available';
import { RepositoryField } from '../factory-editor/basics/repository-field';
import { useOrgConnections } from '../factory-editor/basics/runtime-fields';
import { useSection, useViewOnly } from '../factory-editor/draft/use-draft';
import { SettingsGroup } from './settings-group';

export function RepositorySection() {
  const basics = useSection('basics');
  const viewOnly = useViewOnly();
  const { connections, kindOf } = useOrgConnections();
  const [editing, setEditing] = useState(false);
  const repository = basics.repository ?? undefined;
  const set = Boolean(repository?.url || repository?.connection);
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
        <RepositoryField basics={basics} connections={connections} kindOf={kindOf} />
      ) : (
        <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2">
          <span className="min-w-0 flex-1 truncate text-body">{repository?.url || 'No repository URL'}</span>
          <Chip tone="ok" dot>
            {repository?.connection ? `Connected with ${repository.connection}` : 'Public, no connection'}
          </Chip>
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

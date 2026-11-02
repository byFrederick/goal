// Settings › Repository: the repository the runs clone (basics.repository: its URL and the connection git
// signs in with). It reuses the Definition editor's field, which writes through patchRepository. Base branch
// has no field in the factory definition yet (ADR 0016, decision 4), so it stays "Not available yet".

import { Field, Input } from '../../ui/field';
import { NotAvailable } from '../../ui/not-available';
import { RepositoryField } from '../factory-editor/basics/repository-field';
import { useOrgConnections } from '../factory-editor/basics/runtime-fields';
import { useSection } from '../factory-editor/draft/use-draft';
import { SettingsGroup } from './settings-group';

export function RepositorySection() {
  const basics = useSection('basics');
  const { connections, kindOf } = useOrgConnections();
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
      <RepositoryField basics={basics} connections={connections} kindOf={kindOf} />
    </SettingsGroup>
  );
}

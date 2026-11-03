// Settings › Runtime: the size of each run's box (basics.size) and the mise tools every run installs
// (basics.tools), the model key (the secret in runtime.modelKey) and the environments with the agents each
// serves (docs/design/screens/build/factory-settings.jpg). The model fields of the Definition editor's
// runtime are not here.

import { Link } from '@tanstack/react-router';
import type { SandboxSize } from '../../shared/definition/model';
import { useFactoryParams } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { Chip } from '../../ui/chip';
import { SegmentedControl } from '../../ui/segmented-control';
import { useModelKey } from '../factory-editor/basics/model-key';
import { toolSpec } from '../factory-editor/basics/runtime-fields';
import { TokenField } from '../factory-editor/basics/token-field';
import { setBasics } from '../factory-editor/draft/ops';
import { useDraftSession, useSection, useViewOnly } from '../factory-editor/draft/use-draft';
import { useEnvironmentAgents, useEnvironmentNames } from '../factory-editor/environments/use-environments';
import { SettingsGroup } from './settings-group';

const sizes: { value: SandboxSize; label: string }[] = [
  { value: 'small', label: 'Small' },
  { value: 'medium', label: 'Medium' },
  { value: 'large', label: 'Large' },
];

/** A row of the group: its name, then what it holds and where to change it. */
function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <span className="text-sm font-medium">{label}</span>
      <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2 max-md:[&_a]:inline-flex max-md:[&_a]:min-h-11 max-md:[&_a]:items-center">
        {children}
      </div>
    </div>
  );
}

/** The model key, one chip per environment with its agent count, and the links that change them. */
function KeyAndEnvironments() {
  const org = useOrg();
  const params = useFactoryParams();
  const { secret } = useModelKey();
  const { names } = useEnvironmentNames();
  const agents = useEnvironmentAgents();
  return (
    <>
      <Row label="Model key">
        <span className="font-mono text-body">{secret ?? 'None chosen'}</span>
        <span className="text-secondary text-muted">The secret that gives every run its model</span>
        <Link to="/o/$org/secrets" params={{ org: org.slug }} className="font-control text-secondary">
          Change
        </Link>
      </Row>
      <Row label="Environments">
        {names.map((name) => {
          const count = agents?.[name]?.length ?? 0;
          return (
            <span key={name} className="inline-flex items-center gap-2">
              <Chip>{name}</Chip>
              <span className="text-secondary text-muted">{`${count} ${count === 1 ? 'agent' : 'agents'}`}</span>
            </span>
          );
        })}
        <Link
          to="/o/$org/f/$factory/definition/$section"
          params={{ ...params, section: 'environments' }}
          className="font-control text-secondary"
        >
          Edit
        </Link>
      </Row>
    </>
  );
}

export function RuntimeSection() {
  const session = useDraftSession();
  const viewOnly = useViewOnly();
  const basics = useSection('basics');
  const tools = basics.tools ?? [];
  return (
    <SettingsGroup id="runtime" title="Runtime">
      <div className="flex min-w-0 flex-col gap-2">
        {/* The group's legend names it for screen readers. */}
        <span aria-hidden="true" className="text-sm font-medium">
          Size of each run’s box
        </span>
        <SegmentedControl
          label="Size of each run’s box"
          options={sizes}
          value={basics.size ?? 'medium'}
          readOnly={viewOnly}
          onChange={(size) => session.edit(setBasics(basics, 'size', size), { coalesce: '/basics/size' })}
          className="self-start"
        />
        <p className="m-0 text-secondary text-muted">Small 1 CPU, 2 GiB. Medium 2 CPU, 4 GiB. Large 4 CPU, 8 GiB.</p>
      </div>
      <TokenField
        label="Tools"
        noun="tool"
        values={tools}
        placeholder="name@version…"
        mono
        check={(t) => (toolSpec.test(t) ? undefined : 'A mise tool spec, such as go, go@1.26.10 or npm:@acme/cli@2.')}
        help={
          <>
            mise tool specs, installed after the repository’s own <code className="font-mono">mise.toml</code>.
          </>
        }
        onAdd={(t) => session.edit(setBasics(basics, 'tools', [...tools, t]))}
        onRemove={(t) =>
          session.edit(
            setBasics(
              basics,
              'tools',
              tools.filter((x) => x !== t),
            ),
          )
        }
      />
      <KeyAndEnvironments />
    </SettingsGroup>
  );
}

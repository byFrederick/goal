// Settings › Runtime: a line that points at environments, where a box's size, CLIs and reach live (ADR 0037),
// the model key (the secret in runtime.modelKey) and the environments with the agents each serves
// (docs/design/screens/build/factory-settings.html). The model fields of the Definition editor's runtime are
// not here.

import { Link } from '@tanstack/react-router';
import { useFactoryParams } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { Chip } from '../../ui/chip';
import { touchLinkWide } from '../../ui/touch-target';
import { useModelKey } from '../factory-editor/basics/model-key';
import { useEnvironmentAgents, useEnvironmentNames } from '../factory-editor/environments/use-environments';
import { SettingsGroup } from './settings-group';

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
          className={`${touchLinkWide} font-control text-secondary`}
        >
          Edit
        </Link>
      </Row>
    </>
  );
}

export function RuntimeSection() {
  const org = useOrg();
  const params = useFactoryParams();
  return (
    <SettingsGroup id="runtime" title="Runtime">
      <p className="m-0 text-body">
        Box size, CLIs and what agents reach live in{' '}
        <Link
          to="/o/$org/f/$factory/definition/$section"
          params={{ org: org.slug, factory: params.factory, section: 'environments' }}
          className="text-ink underline underline-offset-2"
        >
          environments
        </Link>
        .
      </p>
      <KeyAndEnvironments />
    </SettingsGroup>
  );
}

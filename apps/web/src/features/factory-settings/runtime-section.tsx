// Settings › Runtime: the size of each run's box (basics.size) and the mise tools every run installs
// (basics.tools). The model fields of the Definition editor's runtime are not here.

import type { SandboxSize } from '../../shared/definition/model';
import { SegmentedControl } from '../../ui/segmented-control';
import { toolSpec } from '../factory-editor/basics/runtime-fields';
import { TokenField } from '../factory-editor/basics/token-field';
import { setBasics } from '../factory-editor/draft/ops';
import { useDraftSession, useSection, useViewOnly } from '../factory-editor/draft/use-draft';
import { SettingsGroup } from './settings-group';

const sizes: { value: SandboxSize; label: string }[] = [
  { value: 'small', label: 'Small' },
  { value: 'medium', label: 'Medium' },
  { value: 'large', label: 'Large' },
];

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
    </SettingsGroup>
  );
}

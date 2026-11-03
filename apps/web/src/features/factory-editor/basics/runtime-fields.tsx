// Basics › Runtime (spec 4 §4.4, §10, canvas Define-Basics and New-Basics): each run is a fresh
// box from the platform's agent image. The model (agents.default_model), the model key (the secret in
// runtime.modelKey), the box's size, the tools mise installs and the repository cloned before the agent
// starts, with the secret git signs in with. B's problems show at their field with their quick fixes, and the forms phase 4 removed
// (runtime.image, runner, auth, secrets, knowledgeBase) are listed with theirs.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { builderMetaQuery } from '../../../api/queries';
import type { BasicsSection, SandboxSize } from '../../../shared/definition/model';
import { useOrg } from '../../../shared/org';
import { Field, NativeSelect } from '../../../ui/field';
import { SegmentedControl } from '../../../ui/segmented-control';
import { setBasics, setDefaultModel } from '../draft/ops';
import { useDraftSession, useSection, useViewOnly } from '../draft/use-draft';
import { ProblemFixes } from '../frame/problem-fixes';
import { EditedMark } from '../frame/section-shell';
import { useProblems } from '../frame/use-editor';
import { useModelKey } from './model-key';
import { RepositoryField } from './repository-field';
import { TokenField } from './token-field';

/** config's toolSpec (spec 4 §4.4): an optional backend, a tool name and an optional @version. */
export const toolSpec = /^(?:[a-z][a-z0-9-]*:)?@?[A-Za-z0-9][A-Za-z0-9._/+-]*(?:@[A-Za-z0-9][A-Za-z0-9._+:-]*)?$/;

const sizes: { value: SandboxSize; label: string }[] = [
  { value: 'small', label: 'Small' },
  { value: 'medium', label: 'Medium' },
  { value: 'large', label: 'Large' },
];

/** The keys phase 4 removed from Basics, whose problems (field-removed) carry their fixes. */
const removedKeys = ['runner', 'image', 'auth', 'secrets', 'knowledge_base'] as const;

export function RuntimeFields({ basics }: { basics: BasicsSection }) {
  const org = useOrg();
  const session = useDraftSession();
  const viewOnly = useViewOnly();
  const agents = useSection('agents');
  const models = useQuery(builderMetaQuery(session.org)).data?.models ?? [];
  const modelKey = useModelKey();
  const removed = removedKeys.some((k) => basics[k] !== undefined);
  const toolProblems = useProblems('/basics/tools');

  const fallback = models.find((m) => m.default);
  const model = agents.default_model ?? '';

  return (
    <div className="col-span-full flex min-w-0 flex-col gap-4 border-t border-line pt-5">
      <div className="flex flex-col gap-1">
        <h3 className="m-0 text-base font-medium">Runtime</h3>
        <p className="m-0 text-sm leading-normal text-muted">
          Each run is a fresh box from Goal’s agent image: Claude Code, git, gh, mise, Go, Node, Python and PostgreSQL.
        </p>
      </div>
      {removed ? (
        <div className="flex flex-col gap-2 rounded-control border border-line bg-card p-3">
          <p className="m-0 text-sm font-medium">Removed in phase 4</p>
          {removedKeys.map((k) => (
            <ProblemFixes key={k} pointer={`/basics/${k}`} />
          ))}
        </div>
      ) : null}
      <div className="grid grid-cols-1 gap-x-5 gap-y-4 @[640px]:grid-cols-2">
        <Field
          label="Model"
          aside={<EditedMark section="agents" pointer="/agents/default_model" label="the model" />}
          help="For agents that don’t name a model."
        >
          <NativeSelect
            value={model}
            onChange={(e) => session.edit(setDefaultModel(agents, e.target.value === '' ? undefined : e.target.value))}
          >
            <option value="">{fallback ? `Factory default (${fallback.label})` : 'Factory default'}</option>
            {model === '' || models.some((m) => m.id === model) ? null : <option value={model}>{model}</option>}
            {models.map((m) => (
              <option key={m.id} value={m.id}>
                {m.label}
              </option>
            ))}
          </NativeSelect>
        </Field>
        <div className="flex min-w-0 flex-col gap-1.5">
          <span className="text-sm font-medium">Model key</span>
          <span className="flex min-h-8 flex-wrap items-center gap-x-3 gap-y-1">
            <span className="font-mono text-body">{modelKey.secret ?? 'None chosen'}</span>
            <Link
              to="/o/$org/secrets"
              params={{ org: org.slug }}
              className="font-control text-secondary max-md:inline-flex max-md:min-h-11 max-md:items-center"
            >
              Change in Secrets
            </Link>
          </span>
          <p className="m-0 text-[13px] leading-snug text-muted">The secret that gives every run its model.</p>
          <ProblemFixes pointer="/basics/model_connection" />
        </div>
        <div className="flex min-w-0 flex-col gap-1.5">
          {/* The group's legend names it for screen readers. */}
          <span aria-hidden="true" className="text-sm font-medium">
            Size of each run’s box
          </span>
          <SegmentedControl
            label="Size of each run’s box"
            options={sizes}
            value={basics.size ?? 'medium'}
            readOnly={viewOnly}
            onChange={(size) => session.edit(setBasics(basics, 'size', size))}
            className="self-start"
          />
          <p className="m-0 text-[13px] leading-snug text-muted">
            Small 1 CPU, 2 GiB. Medium 2 CPU, 4 GiB. Large 4 CPU, 8 GiB.
          </p>
        </div>
        <div className="flex min-w-0 flex-col gap-1.5">
          <TokenField
            label="Tools"
            aside={<EditedMark section="basics" pointer="/basics/tools" label="the tools" />}
            noun="tool"
            values={basics.tools ?? []}
            placeholder="name@version…"
            mono
            check={(t) =>
              toolSpec.test(t) ? undefined : 'A mise tool spec, such as go, go@1.26.10 or npm:@acme/cli@2.'
            }
            help={
              <>
                mise tool specs, installed after the repository’s own <code className="font-mono">mise.toml</code>.
              </>
            }
            onAdd={(t) => session.edit(setBasics(basics, 'tools', [...(basics.tools ?? []), t]))}
            onRemove={(t) =>
              session.edit(
                setBasics(
                  basics,
                  'tools',
                  (basics.tools ?? []).filter((x) => x !== t),
                ),
              )
            }
          />
          {toolProblems.length > 0 ? <ProblemFixes pointer="/basics/tools" /> : null}
        </div>
        <RepositoryField basics={basics} />
      </div>
    </div>
  );
}

// The wizard's Start step (D §10.11, canvas New-Start): four ways to start (Blank, From a
// template, Clone a factory, Import YAML; `?from=` from the Factories home picks one), the panel
// of the chosen way, the new factory's name, and Continue to Basics, which creates the factory
// with B's POST /factories and opens Basics on it. Cancel creates nothing. YAML shows the chosen
// source's factory.yaml, read-only, when there is one to show.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { ChevronRight, Copy, FileText, LayoutTemplate, Plus } from 'lucide-react';
import { useId, useRef, useState } from 'react';
import { ApiError } from '../../../api/client';
import { createFactory } from '../../../api/definitions';
import { factoriesQuery, keys, versionFileQuery, versionQuery } from '../../../api/queries';
import { useOrg } from '../../../shared/org';
import { Button } from '../../../ui/button';
import { Field, Input } from '../../../ui/field';
import { RadioCards } from '../../../ui/radio-cards';
import { Tooltip } from '../../../ui/tooltip';
import { WizardFooter, WizardHeader, WizardViewToggle } from '../frame/wizard-frame';
import { WizardSteps } from '../frame/wizard-steps';
import { recordDone, wizardStorage } from '../wizard-progress';
import { CodeEditor } from '../yaml/code-editor';
import { ClonePicker } from './clone-picker';
import { ImportPanel, type ImportResult } from './import-panel';
import { nameProblem, newFactoryRequest, type StartKind, startKinds, suggestedName } from './start-choice';
import { TemplatePicker, useTemplates } from './template-picker';

export interface StartSearch {
  from?: StartKind | undefined;
  template?: string | undefined;
  mode?: 'yaml' | undefined;
}

export function StartStep({ search }: { search: StartSearch }) {
  const org = useOrg();
  const navigate = useNavigate();
  const client = useQueryClient();
  const nameRef = useRef<HTMLInputElement>(null);
  const reasonId = useId();

  const kind: StartKind = search.from ?? 'template';
  const [clone, setClone] = useState<string>();
  const [imported, setImported] = useState<ImportResult>();
  const [typedName, setTypedName] = useState<string>();
  // The name goald refused as taken ('' for factory.yaml's own, sent without one).
  const [takenName, setTakenName] = useState<string | null>(null);
  const [error, setError] = useState<string>();

  const factories = useQuery(factoriesQuery(org.id)).data ?? [];
  const { blank, templates } = useTemplates();
  // The first template is chosen until another one is (the board's default).
  const template = search.template ?? templates[0]?.name;
  const taken = factories.map((f) => f.name);
  const cloneName = factories.find((f) => f.id === clone)?.name;
  const name = typedName ?? suggestedName(kind, { template, clone: cloneName }, taken);
  const trimmed = name.trim();
  const nameError =
    takenName !== null && takenName === trimmed
      ? trimmed
        ? `${org.name} already has a factory named ${trimmed}. Choose another name.`
        : `${org.name} already has a factory with factory.yaml’s name. Name the new factory here.`
      : kind === 'yaml' && imported?.verdict.nameTaken && trimmed === ''
        ? `${org.name} already has a factory with factory.yaml’s name. Name the new factory here.`
        : trimmed !== '' && typedName !== undefined
          ? (nameProblem(trimmed) ?? undefined)
          : undefined;

  const request = newFactoryRequest({
    kind,
    name,
    template,
    clone,
    files: imported?.verdict.ok ? imported.files : undefined,
  });
  const reason =
    kind === 'template' && !template
      ? 'Choose a template first.'
      : kind === 'clone' && !clone
        ? 'Choose a factory to copy first.'
        : kind === 'yaml' && !imported?.verdict.ok
          ? 'Paste or upload a factory.yaml that matches the schema first.'
          : !request || (trimmed !== '' && nameProblem(trimmed))
            ? 'Give the factory a name first.'
            : undefined;

  const create = useMutation({
    mutationFn: () => createFactory(org.id, request ?? { start: { kind: 'blank' } }),
    onSuccess: (factory) => {
      recordDone(wizardStorage(), factory.id, 'start');
      void client.invalidateQueries({ queryKey: keys.factories(org.id) });
      void client.invalidateQueries({ queryKey: keys.overviews(org.id) });
      void navigate({
        to: '/o/$org/new/$step',
        params: { org: org.slug, step: 'basics' },
        search: { factory: factory.id },
      });
    },
    onError: (err) => {
      if (err instanceof ApiError && err.code === 'name-taken') {
        setTakenName(trimmed);
        setError(undefined);
        nameRef.current?.focus();
        return;
      }
      setError(err instanceof ApiError ? (err.detail ?? err.title) : 'goald can’t be reached. Try again in a moment.');
    },
  });

  /** Sets the Start step's search params (the choice, the template, the view), replacing the entry. */
  const setStart = (patch: Partial<Record<'from' | 'template' | 'mode', string | undefined>>) =>
    void navigate({
      to: '.',
      search: (prev: Record<string, unknown>) => {
        const next: Record<string, unknown> = { ...prev, ...patch };
        for (const [k, v] of Object.entries(next)) if (v === undefined) delete next[k];
        return next as never;
      },
      replace: true,
    });

  const choose = (from: StartKind) => {
    setError(undefined);
    setStart({ from, mode: undefined });
  };

  const yaml = search.mode === 'yaml';
  const yamlUnavailable =
    kind === 'clone' && clone
      ? undefined
      : kind === 'yaml'
        ? 'the factory.yaml you import is in the editor below'
        : kind === 'clone'
          ? 'choose a factory to copy first'
          : 'a template’s files open in YAML after you continue';

  return (
    <>
      <WizardHeader
        title="New factory"
        description="Everything about a factory is set here: its line, agents, prompts, skills and connections. Nothing goes in code or environment variables. Your draft saves as you go."
        actions={
          <WizardViewToggle
            yaml={yaml && !yamlUnavailable}
            yamlUnavailable={yamlUnavailable}
            onChange={(y) => setStart({ mode: y ? 'yaml' : undefined })}
          />
        }
      />
      <WizardSteps current="start" done={new Set()} />

      {yaml && !yamlUnavailable && clone ? (
        <ClonePreview factory={clone} />
      ) : (
        <>
          <RadioCards
            legend="How do you want to start?"
            name="start"
            value={kind}
            onChange={choose}
            options={startKinds.map((k) => startOptions(k, Boolean(blank)))}
          />

          {kind === 'template' ? (
            <TemplatePicker value={template} onChange={(template) => setStart({ template })} />
          ) : null}
          {kind === 'clone' ? <ClonePicker value={clone} onChange={setClone} /> : null}
          {kind === 'yaml' ? <ImportPanel onResult={setImported} /> : null}
          {kind === 'blank' ? (
            <p className="m-0 max-w-[72ch] text-[15px] leading-normal text-ink-soft">
              {blank
                ? `${blank.summary}. Build the rest in the next steps.`
                : 'An empty line. Add stations next, then the agents who work at them.'}
            </p>
          ) : null}
        </>
      )}

      <div className="max-w-md">
        <Field
          label="Name"
          error={nameError}
          help={
            kind === 'yaml'
              ? 'Leave it empty to use the name in factory.yaml. Lowercase, used in links and the sidebar.'
              : 'Lowercase, used in links and the sidebar. You can change it in Basics.'
          }
        >
          <Input
            ref={nameRef}
            value={name}
            onChange={(e) => setTypedName(e.target.value)}
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            translate="no"
            className="font-mono md:text-sm"
          />
        </Field>
      </div>

      {error ? (
        <p
          role="alert"
          className="m-0 max-w-2xl rounded-control border border-rework-tint bg-rework-tint px-4 py-3 text-sm"
        >
          The factory wasn’t created: {error.replace(/\.$/, '')}.
        </p>
      ) : null}

      <WizardFooter
        step="start"
        start={
          <Button asChild variant="white" size="lg" className="md:h-10 md:text-sm">
            <Link to="/o/$org" params={{ org: org.slug }}>
              Cancel
            </Link>
          </Button>
        }
        end={
          <ContinueButton
            reason={reason ?? (nameError ? 'Fix the name first.' : undefined)}
            reasonId={reasonId}
            pending={create.isPending}
            onClick={() => create.mutate()}
          />
        }
      />
    </>
  );
}

function startOptions(kind: StartKind, hasBlankTemplate: boolean) {
  const icon = { size: 18, strokeWidth: 1.5 } as const;
  switch (kind) {
    case 'blank':
      return {
        value: kind,
        label: 'Blank',
        description: hasBlankTemplate
          ? 'An inbox, one agent station and done. Build the rest yourself.'
          : 'An empty line. Add stations next.',
        icon: <Plus {...icon} />,
      };
    case 'template':
      return {
        value: kind,
        label: 'From a template',
        description: 'Templates are factories saved as data. Pick one, then change anything.',
        icon: <LayoutTemplate {...icon} />,
      };
    case 'clone':
      return {
        value: kind,
        label: 'Clone a factory',
        description: 'Copy a factory of this organization without its items, history or credentials.',
        icon: <Copy {...icon} />,
      };
    case 'yaml':
      return {
        value: kind,
        label: 'Import YAML',
        description: 'Upload or paste a factory.yaml. It’s checked against the schema first.',
        icon: <FileText {...icon} />,
      };
  }
}

/** Continue to Basics: disabled with its reason said (tooltip and description) until the choice is complete. */
function ContinueButton({
  reason,
  reasonId,
  pending,
  onClick,
}: {
  reason: string | undefined;
  reasonId: string;
  pending: boolean;
  onClick: () => void;
}) {
  const button = (
    <Button
      variant="primary"
      size="lg"
      className="md:h-10 md:text-sm"
      aria-disabled={reason || pending ? true : undefined}
      aria-describedby={reason ? reasonId : undefined}
      onClick={() => (reason || pending ? undefined : onClick())}
    >
      {pending ? 'Creating…' : 'Continue to Basics'}
      <ChevronRight size={14} strokeWidth={1.75} aria-hidden="true" />
      {reason ? (
        <span id={reasonId} className="sr-only">
          {reason}
        </span>
      ) : null}
    </Button>
  );
  return reason ? (
    <Tooltip label={reason} side="top">
      {button}
    </Tooltip>
  ) : (
    button
  );
}

/** The factory.yaml a clone starts from: the running version's, read-only. */
function ClonePreview({ factory }: { factory: string }) {
  const org = useOrg();
  const version = useQuery(versionQuery(org.id, factory, 'current')).data;
  const sha = version?.files.find((f) => f.path === 'factory.yaml')?.sha256 ?? '';
  const file = useQuery({ ...versionFileQuery(org.id, factory, 'current', 'factory.yaml', sha), enabled: sha !== '' });
  return (
    <section
      aria-label="factory.yaml of the factory to copy"
      className="overflow-hidden rounded-card border border-line bg-card"
    >
      <CodeEditor
        value={file.data?.text ?? ''}
        onChange={() => undefined}
        language="yaml"
        label={`factory.yaml of ${version ? `v${version.number ?? ''}` : 'the factory to copy'}, read-only`}
        readOnly
        minLines={16}
        maxLines={40}
      />
    </section>
  );
}

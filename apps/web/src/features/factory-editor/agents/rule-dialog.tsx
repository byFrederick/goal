// The Add rule and Edit rule dialog (ADR 0014). A rule is its description, whom it applies to
// (agents, stations, lanes and work types of this definition; none checked means every agent),
// the paths it is scoped to, and its markdown body, written in a textarea or previewed. Saving
// is one agents save: the rules entry and rules/<name>.md (addRule, or setRuleText on an edit).
// A rule's name is set when it is added; to rename one, delete it and add it again.
import { type FormEvent, Suspense, useEffect, useId, useState } from 'react';
import { ApiError } from '../../../api/client';
import type { AgentsSection, BasicsSection, LineSection, ModelRule } from '../../../shared/definition/model';
import { parseRule, type RuleScope, type RuleText, serializeRule } from '../../../shared/definition/rule-md';
import { lazyRetrying } from '../../../shared/lazy';
import { Button } from '../../../ui/button';
import { cn } from '../../../ui/cn';
import { Dialog, DialogClose, DialogContent } from '../../../ui/dialog';
import { Field, Input, Textarea } from '../../../ui/field';
import { SegmentedControl } from '../../../ui/segmented-control';
import { addRule, ruleFile, setRuleText, slug } from '../draft/ops';
import { useDraftSession, useSection } from '../draft/use-draft';

const MarkdownPreview = lazyRetrying(() => import('./markdown-preview').then((m) => m.default));

const ruleName = /^[a-z][a-z0-9-]{1,38}[a-z0-9]$/;
const empty: RuleText = { description: '', body: '' };

type Option = { value: string; label: string };

/** The choices of one kind, plus any value the rule names that this definition no longer has. */
function Choices({
  legend,
  help,
  options,
  selected,
  mono = false,
  onChange,
}: {
  legend: string;
  help: string;
  options: Option[];
  selected: string[];
  mono?: boolean;
  onChange: (next: string[]) => void;
}) {
  const helpId = useId();
  const known = new Set(options.map((o) => o.value));
  const all = [...options, ...selected.filter((v) => !known.has(v)).map((v) => ({ value: v, label: v }))];
  if (all.length === 0) return null;
  return (
    <fieldset aria-describedby={helpId} className="m-0 flex min-w-0 flex-col gap-2 border-0 p-0">
      <legend className="mb-1 text-sm font-medium">{legend}</legend>
      <p id={helpId} className="m-0 text-[13px] text-muted">
        {help}
      </p>
      <div className="flex flex-wrap gap-2">
        {all.map((o) => (
          <label
            key={o.value}
            className="inline-flex min-h-11 cursor-pointer items-center gap-2 rounded-chip border border-line bg-card px-3 text-[13px] has-[input:focus-visible]:outline-2 has-[input:focus-visible]:outline-offset-2 has-[input:focus-visible]:outline-run md:min-h-8"
          >
            <input
              type="checkbox"
              className="size-4 accent-ink"
              checked={selected.includes(o.value)}
              onChange={() =>
                onChange(selected.includes(o.value) ? selected.filter((v) => v !== o.value) : [...selected, o.value])
              }
            />
            <span translate="no" className={cn(mono && 'font-mono')}>
              {o.label}
            </span>
          </label>
        ))}
      </div>
    </fieldset>
  );
}

/** The values a rule can name here: this definition's agents, stations, lanes and work types. */
function optionsOf(model: { agents: AgentsSection; line: LineSection; basics: BasicsSection }) {
  const lanes = new Set<string>();
  for (const s of model.line.stations ?? []) for (const lane of Object.keys(s.route ?? {})) lanes.add(lane);
  return {
    agents: (model.agents.agents ?? []).map((a) => ({ value: a.name, label: a.name })),
    stations: (model.line.stations ?? []).map((s) => ({ value: s.id, label: s.name || s.id })),
    lanes: [...lanes].sort().map((lane) => ({ value: lane, label: lane })),
    workTypes: (model.basics.work_types ?? []).map((t) => ({ value: t, label: t })),
  };
}

function RuleFields({ rule, initial, onDone }: { rule: ModelRule | undefined; initial: RuleText; onDone: () => void }) {
  const session = useDraftSession();
  const agents = useSection('agents');
  const line = useSection('line');
  const basics = useSection('basics');
  const options = optionsOf({ agents, line, basics });
  const taken = (agents.rules ?? []).map((r) => r.name);
  const [name, setName] = useState('');
  const [description, setDescription] = useState(initial.description);
  const [scope, setScope] = useState<RuleScope>(initial.appliesTo ?? {});
  const [paths, setPaths] = useState((initial.paths ?? []).join('\n'));
  const [body, setBody] = useState(initial.body);
  const [view, setView] = useState<'write' | 'preview'>('write');
  const [error, setError] = useState<string>();

  const pick = (key: keyof RuleScope) => (next: string[]) => setScope({ ...scope, [key]: next });

  const save = () => {
    const text = description.trim();
    if (!text) return setError('Describe the rule in one line: what it is for.');
    let key = rule?.name ?? '';
    if (!rule) {
      const typed = name.trim();
      if (!/[a-z0-9]/i.test(typed)) return setError('Give the rule a name.');
      key = slug(typed);
      if (!ruleName.test(key)) {
        return setError('Lowercase letters, digits and dashes, 3 to 40 characters, starting with a letter.');
      }
      if (taken.includes(key)) return setError(`A rule named ${key} already exists.`);
    }
    const globs = paths
      .split('\n')
      .map((p) => p.trim())
      .filter(Boolean);
    const file = serializeRule({ description: text, appliesTo: scope, paths: globs, body });
    session.edit(rule ? setRuleText(agents, key, file) : addRule(agents, key, file));
    onDone();
  };

  const typedName = slug(name.trim());

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e: FormEvent) => {
        e.preventDefault();
        save();
      }}
    >
      {rule ? (
        <Field label="Name" help="A rule's name is set when it is added. To rename it, delete it and add it again.">
          <Input value={rule.name} readOnly translate="no" className="font-mono" />
        </Field>
      ) : (
        <Field
          label="Name"
          help={name.trim() ? `The rule is saved as ${ruleFile(typedName)}.` : 'Lowercase words, such as secrets.'}
        >
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            autoComplete="off"
            spellCheck={false}
            translate="no"
            className="font-mono"
          />
        </Field>
      )}

      <Field label="Description" help="One line: what the rule is for. Agents see it with the rule.">
        <Input value={description} onChange={(e) => setDescription(e.target.value)} />
      </Field>

      <div className="flex flex-col gap-4 rounded-control border border-line bg-card p-3">
        <p className="m-0 text-[13px] text-muted">
          Leave every box unchecked and the rule applies to every agent. Otherwise it applies to a run only when each
          kind you check holds the run’s value.
        </p>
        <Choices
          legend="Agents"
          help="The agents that get the rule."
          options={options.agents}
          selected={scope.agents ?? []}
          onChange={pick('agents')}
        />
        <Choices
          legend="Stations"
          help="The stations whose agents get the rule."
          options={options.stations}
          selected={scope.stations ?? []}
          onChange={pick('stations')}
        />
        <Choices
          legend="Lanes"
          help="The lanes a station routes to its agent."
          options={options.lanes}
          selected={scope.lanes ?? []}
          onChange={pick('lanes')}
        />
        <Choices
          legend="Work types"
          help="The kinds of work the rule is for."
          options={options.workTypes}
          selected={scope.workTypes ?? []}
          onChange={pick('workTypes')}
        />
      </div>

      <Field
        label="Paths"
        help="Globs, one per line, such as src/**/*.ts. The rule loads only when an agent works on matching files. Empty: every file."
      >
        <Textarea value={paths} onChange={(e) => setPaths(e.target.value)} rows={2} className="font-mono text-sm" />
      </Field>

      <Field
        label="Rule text"
        aside={
          <SegmentedControl
            label="Rule text view"
            value={view}
            onChange={setView}
            options={[
              { value: 'write', label: 'Write' },
              { value: 'preview', label: 'Preview' },
            ]}
          />
        }
        help="Markdown, short. Put what a check can enforce into a contract, a gate, a tool permission or branch protection; keep here only the judgment those cannot express. In this factory's runs, factory rules win over repository rules."
      >
        {view === 'preview' ? (
          <div className="min-h-[160px] overflow-hidden rounded-control border border-rule bg-card">
            <Suspense fallback={<p className="m-0 p-3 text-sm text-muted">Loading the preview…</p>}>
              <MarkdownPreview text={body} />
            </Suspense>
          </div>
        ) : (
          <Textarea value={body} onChange={(e) => setBody(e.target.value)} rows={8} className="font-mono text-sm" />
        )}
      </Field>

      {error ? (
        <p role="alert" className="m-0 text-[13px] leading-snug text-rework-ink">
          {error}
        </p>
      ) : null}

      <div className="flex flex-wrap justify-end gap-2">
        <DialogClose asChild>
          <Button variant="secondary" size="lg" className="md:h-10 md:text-sm">
            Cancel
          </Button>
        </DialogClose>
        <Button type="submit" variant="primary" size="lg" className="md:h-10 md:text-sm">
          {rule ? 'Save rule' : 'Add rule'}
        </Button>
      </div>
    </form>
  );
}

const missingIsFine = (err: unknown) => {
  if (!(err instanceof ApiError && err.code === 'file-not-found')) throw err;
  return '';
};

/** Loads the rule's text through the session (so its undo can write it back), then shows the form. */
function RuleForm({ rule, onDone }: { rule: ModelRule | undefined; onDone: () => void }) {
  const session = useDraftSession();
  const [loaded, setLoaded] = useState<RuleText | null>(rule ? null : empty);
  const [failed, setFailed] = useState(false);
  const name = rule?.name;

  useEffect(() => {
    if (name === undefined) return;
    let live = true;
    session
      .fileText(ruleFile(name))
      .catch(missingIsFine)
      .then(
        (text) => live && setLoaded(parseRule(text)),
        () => live && setFailed(true),
      );
    return () => {
      live = false;
    };
  }, [name, session]);

  if (failed) {
    return (
      <p role="alert" className="m-0 text-sm text-rework-ink">
        The rule could not be read. Try again.
      </p>
    );
  }
  if (!loaded) {
    return (
      <p role="status" aria-busy="true" className="m-0 text-sm text-muted">
        Loading the rule…
      </p>
    );
  }
  return <RuleFields rule={rule} initial={loaded} onDone={onDone} />;
}

export function RuleDialog({
  rule,
  open,
  onOpenChange,
}: {
  /** The rule to edit; undefined adds one. */
  rule: ModelRule | undefined;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={rule ? `Edit ${rule.name}` : 'Add a rule'}
        description="Rules are always on for the agents they apply to, unlike skills, which load on demand."
      >
        {open ? <RuleForm rule={rule} onDone={() => onOpenChange(false)} /> : null}
      </DialogContent>
    </Dialog>
  );
}

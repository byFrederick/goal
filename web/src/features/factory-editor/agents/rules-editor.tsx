// The factory's rules (ADR 0014), in the Agents section below the agents: each rule's name, its
// description and whom it applies to, with Edit and Delete, and Add. Rules are always on for the
// agents they target, unlike skills; this area says what that means and where a mechanism is
// better than a rule. Each rule's text is read through the session, so the list shows what is
// typed; delete saves the rules entry and the file in one agents save, with the file read first
// so its undo can write it back. Read-only sessions show the list only.
import { useEffect, useId, useState } from 'react';
import { toast } from 'sonner';
import { ApiError } from '../../../api/client';
import type { ModelRule } from '../../../shared/definition/model';
import { parseRule, type RuleScope, type RuleText } from '../../../shared/definition/rule-md';
import { Button } from '../../../ui/button';
import { ConfirmDialog } from '../../../ui/confirm-dialog';
import { EmptyState } from '../../../ui/empty-state';
import { removeRule, ruleFile } from '../draft/ops';
import { useDraft, useDraftSession, useSection, useViewOnly } from '../draft/use-draft';
import { useProblems } from '../frame/use-editor';
import { RuleDialog } from './rule-dialog';

/** The rule's text as the session has it: undefined while it loads, null when its file is missing or unreadable. */
function useRuleText(name: string): string | null | undefined {
  const session = useDraftSession();
  const typed = useDraft((s) => s.files.get(ruleFile(name))?.text);
  const [loaded, setLoaded] = useState<string | null>();

  useEffect(() => {
    let live = true;
    session.fileText(ruleFile(name)).then(
      (text) => live && setLoaded(text),
      () => live && setLoaded(null),
    );
    return () => {
      live = false;
    };
  }, [name, session]);

  return typed ?? loaded;
}

/** Whom the rule applies to, as the list says it: "Every agent", or the agents and the other kinds it names. */
export function appliesLabel(scope: RuleScope | undefined, paths: string[] | undefined): string {
  const parts = [
    scope?.agents?.length ? scope.agents.join(', ') : 'Every agent',
    scope?.stations?.length ? `stations ${scope.stations.join(', ')}` : undefined,
    scope?.lanes?.length ? `lanes ${scope.lanes.join(', ')}` : undefined,
    scope?.workTypes?.length ? `work types ${scope.workTypes.join(', ')}` : undefined,
    paths?.length ? `files ${paths.join(', ')}` : undefined,
  ];
  return parts.filter(Boolean).join(' · ');
}

function RuleRow({
  rule,
  problem,
  onEdit,
  onDelete,
}: {
  rule: ModelRule;
  problem: string | undefined;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const viewOnly = useViewOnly();
  const text = useRuleText(rule.name);
  const parsed: RuleText | undefined = text ? parseRule(text) : undefined;

  return (
    <li className="flex flex-col gap-2 border-t border-line py-3 first:border-t-0 first:pt-0 md:flex-row md:items-start md:justify-between md:gap-4">
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span translate="no" className="font-mono text-[13px] break-all">
          {rule.name}
        </span>
        {text === undefined ? <span className="text-[15px] text-muted">Loading…</span> : null}
        {text === null ? <span className="text-[15px] text-rework-ink">The rule’s file is missing.</span> : null}
        {parsed ? (
          <>
            <span className="text-[15px]">{parsed.description || 'No description'}</span>
            <span className="text-[13px] text-muted">{appliesLabel(parsed.appliesTo, parsed.paths)}</span>
          </>
        ) : null}
        {problem ? <span className="text-[13px] leading-snug text-rework-ink">{problem}</span> : null}
      </div>
      {viewOnly ? null : (
        <div className="flex shrink-0 gap-2">
          <Button
            variant="secondary"
            size="sm"
            className="min-h-11 md:min-h-0"
            aria-label={`Edit ${rule.name}`}
            onClick={onEdit}
          >
            Edit
          </Button>
          <Button
            variant="ghost"
            size="sm"
            className="min-h-11 md:min-h-0"
            aria-label={`Delete ${rule.name}`}
            onClick={onDelete}
          >
            Delete
          </Button>
        </div>
      )}
    </li>
  );
}

export function RulesEditor() {
  const session = useDraftSession();
  const viewOnly = useViewOnly();
  const agents = useSection('agents');
  const rules = agents.rules ?? [];
  const problems = useProblems('/agents/rules');
  const headingId = useId();
  const [editor, setEditor] = useState<{ rule: ModelRule | undefined } | null>(null);
  const [removing, setRemoving] = useState<ModelRule>();

  const problemAt = (i: number) =>
    problems.find((p) => p.pointer === `/agents/rules/${i}` || p.pointer?.startsWith(`/agents/rules/${i}/`))?.message;
  const others = problems.filter((p) => !/^\/agents\/rules\/\d+/.test(p.pointer ?? ''));

  const remove = async (rule: ModelRule) => {
    // Undo puts the file back, so its text is read before it goes; a missing file has none.
    try {
      await session.fileText(ruleFile(rule.name));
    } catch (err) {
      if (!(err instanceof ApiError && err.code === 'file-not-found')) {
        toast.error('The rule could not be read, so nothing was removed. Try again.');
        return;
      }
    }
    session.edit(removeRule(agents, rule.name));
    setRemoving(undefined);
    toast.success(`${rule.name} removed`);
  };

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-3 rounded-card border border-line bg-card p-3.5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 id={headingId} className="m-0 text-[15px] font-medium">
          Rules
        </h3>
        {viewOnly ? null : (
          <Button
            variant="secondary"
            size="sm"
            className="min-h-11 md:min-h-0"
            onClick={() => setEditor({ rule: undefined })}
          >
            Add a rule
          </Button>
        )}
      </div>
      <p className="m-0 text-[13px] leading-snug text-muted">
        Rules are always on for the agents they apply to, unlike skills, which load on demand. Prefer a mechanism for
        anything that can be checked: a contract, a gate, a tool permission or branch protection. Keep rule text for
        judgment those cannot express. In this factory’s runs, factory rules win over repository rules.
      </p>

      {rules.length > 0 ? (
        <ul className="m-0 flex list-none flex-col p-0">
          {rules.map((rule, i) => (
            <RuleRow
              key={rule.name}
              rule={rule}
              problem={problemAt(i)}
              onEdit={() => setEditor({ rule })}
              onDelete={() => setRemoving(rule)}
            />
          ))}
        </ul>
      ) : (
        <EmptyState title="No rules yet">
          A rule tells every agent it applies to how this factory works, such as how a change is delivered or how
          secrets are handled.
        </EmptyState>
      )}
      {others.map((p) => (
        <p key={`${p.code}:${p.pointer}`} className="m-0 text-[13px] leading-snug text-rework-ink">
          {p.message}
        </p>
      ))}

      {viewOnly ? null : (
        <>
          <RuleDialog
            rule={editor?.rule}
            open={editor !== null}
            onOpenChange={(open) => {
              if (!open) setEditor(null);
            }}
          />
          <ConfirmDialog
            open={removing !== undefined}
            onOpenChange={(open) => {
              if (!open) setRemoving(undefined);
            }}
            title={`Delete ${removing?.name ?? 'the rule'}?`}
            description={`Its file ${removing ? ruleFile(removing.name) : ''} is removed in the same save. Agents stop getting the rule from the next run on.`}
            confirmLabel="Delete rule"
            tone="danger"
            onConfirm={() => {
              if (removing) void remove(removing);
            }}
          />
        </>
      )}
    </section>
  );
}

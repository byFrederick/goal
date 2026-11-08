// The standing rules (Always allow, ADR 0037 decision 8) of one MCP server, under the tool group that holds
// their tool, each with Revoke. The organization's active rules come from one query; the ones of this
// factory, this environment and this server are picked by factory id (never by name), environment and server.
// Revoke needs standing_rule:manage and a sign-in from the last 10 minutes (session.ts opens the re-auth dialog
// and sends the request again). A revoked row leaves with an opacity and height collapse (MOTION.md), and a
// 409 standing-rule-revoked is read to the person as goald words it, with the list refreshed.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { CircleCheck, Info } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { ApiError, revokeStandingRule } from '../../../api/client';
import { keys, standingRulesQuery } from '../../../api/queries';
import type { StandingRule } from '../../../api/types';
import { formatDay } from '../../../shared/format';
import { useOrg, useOrgHolds } from '../../../shared/org';
import { Button } from '../../../ui/button';
import { cn } from '../../../ui/cn';
import { Skeleton, SkeletonRows } from '../../../ui/skeleton';
import { Tooltip } from '../../../ui/tooltip';
import { useDraftSession } from '../draft/use-draft';

/** Mirrors --duration-quick in styles.css: how long a revoked row stays while it collapses. */
const EXIT_MS = 150;

/** The active rules keyed by this factory's id, this environment and this server; loading while the list waits. */
export function useServerRules(environment: string, server: string): { rules: StandingRule[]; loading: boolean } {
  const org = useOrg();
  const factory = useDraftSession().factory; // the draft's factory id: Definition and the wizard both open one
  const query = useQuery(standingRulesQuery(org.id));
  const rules = (query.data ?? []).filter(
    (r) => r.revoked_at === null && r.factory_id === factory && r.environment === environment && r.server === server,
  );
  return { rules, loading: query.isPending };
}

/** The rows of the rules whose tool is `tools`' (or, with no list, every rule), or nothing. */
export function RuleList({ rules }: { rules: StandingRule[] }) {
  if (rules.length === 0) return null;
  return (
    <ul className="m-0 flex list-none flex-col p-0">
      {rules.map((rule) => (
        <RuleRow key={rule.id} rule={rule} />
      ))}
    </ul>
  );
}

/** A placeholder in the slot of the rule rows while the list loads (MOTION.md, Loading). */
export function RulesPlaceholder() {
  return (
    <Skeleton ready={false} placeholder={<SkeletonRows rows={1} />}>
      {null}
    </Skeleton>
  );
}

function RuleRow({ rule }: { rule: StandingRule }) {
  const org = useOrg();
  const canRevoke = useOrgHolds('standing_rule:manage');
  const client = useQueryClient();
  const [leaving, setLeaving] = useState(false);
  // The list is refreshed once the row has left, so the exit plays; an unmount first refreshes it at once.
  const refresh = useRef<(() => void) | undefined>(undefined);
  useEffect(() => () => refresh.current?.(), []);
  const refetch = () => {
    refresh.current = undefined;
    void client.invalidateQueries({ queryKey: keys.standingRules(org.id) });
  };
  const revoke = useMutation({
    mutationFn: () => revokeStandingRule(org.id, rule.id),
    onSuccess: () => {
      toast.success(`${rule.agent} asks again before calling ${rule.tool}`);
      setLeaving(true);
      refresh.current = refetch;
      setTimeout(() => refresh.current?.(), EXIT_MS);
    },
    onError: (err) => {
      toast.error(err instanceof ApiError ? (err.detail ?? err.title) : 'Could not revoke the rule');
      refetch();
    },
  });
  return (
    <li
      data-leaving={leaving ? '' : undefined}
      className={cn(
        'grid grid-rows-[1fr] transition-[grid-template-rows,opacity] duration-(--duration-quick) ease-out motion-reduce:transition-none',
        leaving && 'grid-rows-[0fr] opacity-0',
      )}
    >
      <div className="min-h-0 overflow-hidden">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2 py-1 text-secondary text-ink-soft">
          <CircleCheck size={14} aria-hidden="true" className="shrink-0" />
          <span className="min-w-0 flex-1 basis-60 break-words">
            <span className="font-mono text-ink">{rule.tool}</span>: Always allowed for {rule.agent}, by{' '}
            {rule.created_by.name} on {formatDay(rule.created_at)}
          </span>
          <Tooltip
            side="top"
            label="Always allow"
            detail="This role no longer asks before this tool, here only. Revoke makes it ask again from its next run."
          >
            <button
              type="button"
              aria-label={`About the rule for ${rule.tool}`}
              className="press inline-flex size-11 items-center justify-center text-muted hover:text-ink md:size-6"
            >
              <Info size={14} aria-hidden="true" />
            </button>
          </Tooltip>
          {canRevoke ? (
            <Button
              variant="danger"
              size="sm"
              disabled={leaving}
              busy={revoke.isPending}
              busyLabel="Revoking…"
              onClick={() => revoke.mutate()}
              aria-label={`Revoke ${rule.tool} for ${rule.agent}`}
            >
              Revoke
            </Button>
          ) : null}
        </div>
      </div>
    </li>
  );
}

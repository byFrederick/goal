// Small pieces the Knowledge pages share: a source's kind with its icon, a status with an icon
// and words (never colour alone), the factories that may search a source, and Sync now.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Check, LoaderCircle } from 'lucide-react';
import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import { syncKnowledgeSource } from '../../api/knowledge';
import { factoriesQuery, keys } from '../../api/queries';
import type { KnowledgeSource, KnowledgeSourceKind } from '../../api/types';
import { FactoryBadge } from '../../app/factory-badge';
import type { Tone } from '../../shared/connections';
import { sourceKinds } from '../../shared/knowledge';
import { factoryMark } from '../../shared/marks';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { ChipText, Dot } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { refusalText, type StatusLine } from './words';

const dotTone: Record<Tone, string> = { ok: 'bg-ok', rework: 'bg-rework', muted: 'bg-muted' };

/** Upload, Synced or Live with its icon. */
export function KindLabel({ kind, className }: { kind: KnowledgeSourceKind; className?: string }) {
  const { label, icon: Icon } = sourceKinds[kind];
  return (
    <span className={cn('inline-flex items-center gap-2 whitespace-nowrap', className)}>
      <Icon size={15} strokeWidth={1.5} aria-hidden="true" className="shrink-0" />
      {label}
    </span>
  );
}

/**
 * A status as a dot and words ("Synced 14:00"), in ink, never colour alone; a spinner while it syncs;
 * the reason under it. A changed status swaps in place (MOTION.md, A status changes in place).
 */
export function StatusText({
  line,
  time,
  busy = false,
  className,
}: {
  line: StatusLine;
  /** When it happened, after the words: "14:00". */
  time?: string | undefined;
  busy?: boolean;
  className?: string | undefined;
}) {
  return (
    <span className={cn('flex min-w-0 flex-col gap-1', className)}>
      <span className="inline-flex items-center gap-2 text-ink">
        {busy ? (
          <LoaderCircle size={14} strokeWidth={1.75} aria-hidden="true" className="shrink-0 motion-safe:animate-spin" />
        ) : (
          <Dot className={cn('size-1.5', dotTone[line.tone])} />
        )}
        <span>
          <ChipText text={time ? `${line.text} ${time}` : line.text} />
        </span>
      </span>
      {line.detail ? <span className="text-secondary [overflow-wrap:anywhere] text-muted">{line.detail}</span> : null}
    </span>
  );
}

/** The factories whose current version may search it, as letter badges with their names; "Nothing yet" for none. */
export function UsedBy({ source }: { source: KnowledgeSource }) {
  const org = useOrg();
  const all = useQuery(factoriesQuery(org.id)).data ?? [];
  const named = source.used_by.factories;
  if (named.length === 0) return <span className="text-muted">Nothing yet</span>;
  const siblings = all.length > 0 ? all : named;
  return (
    <span className="flex flex-wrap items-center gap-x-3 gap-y-1 text-secondary text-ink-soft">
      {named.map((f) => (
        <span key={f.id} className="inline-flex items-center gap-2">
          <FactoryBadge mark={factoryMark(f, siblings)} />
          <span translate="no">{f.name}</span>
        </span>
      ))}
    </span>
  );
}

/**
 * Sync now (spec 4 §8.2): the source is due at once; the stream says when its sync starts and
 * ends, which re-reads the source.
 */
export function useSyncSource(org: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (source: KnowledgeSource) => syncKnowledgeSource(org, source.name),
    onSuccess: async (_, source) => {
      toast.success(`${source.title} syncs now`, { description: 'Its status changes when the sync starts and ends.' });
      await client.invalidateQueries({ queryKey: keys.knowledge(org) });
    },
    onError: (err, source) => toast.error(`Couldn’t sync ${source.title}`, { description: refusalText(err) }),
  });
}

/** The Sync now (or Retry now) button of a source, busy while its request is in flight. */
export function SyncNowButton({
  source,
  sync,
  label = 'Sync now',
  className,
}: {
  source: KnowledgeSource;
  sync: ReturnType<typeof useSyncSource>;
  label?: string;
  className?: string;
}) {
  const pending = sync.isPending && sync.variables?.name === source.name;
  const accepted = sync.isSuccess && sync.variables?.name === source.name;
  const [done, setDone] = useState(false);
  // The label swaps to a check for a moment after Sync now is accepted (MOTION.md, Approve, publish, merge succeed).
  useEffect(() => {
    if (!accepted) return;
    setDone(true);
    const timer = setTimeout(() => setDone(false), 1500);
    return () => clearTimeout(timer);
  }, [accepted]);
  return (
    <Button
      variant="white"
      size="sm"
      className={className}
      disabled={pending}
      aria-label={`${pending ? 'Asking…' : label}, ${source.title}`}
      onClick={() => sync.mutate(source)}
    >
      {done && !pending ? (
        <Check size={16} strokeWidth={1.75} aria-hidden="true" className="animate-success-check text-ok-ink" />
      ) : null}
      <span key={pending ? 'asking' : 'idle'} className="animate-text-states-swap">
        {pending ? 'Asking…' : label}
      </span>
    </Button>
  );
}

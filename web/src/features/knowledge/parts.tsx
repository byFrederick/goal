// Small pieces the Knowledge pages share: a source's kind with its icon, a status with an icon
// and words (never colour alone), the factories that may search a source, and Sync now.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Check, Info, LoaderCircle, TriangleAlert } from 'lucide-react';
import { toast } from 'sonner';
import { syncKnowledgeSource } from '../../api/knowledge';
import { keys } from '../../api/queries';
import type { KnowledgeSource, KnowledgeSourceKind } from '../../api/types';
import { toneText } from '../../shared/connections';
import { sourceKinds } from '../../shared/knowledge';
import { Button } from '../../ui/button';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { refusalText, type StatusLine } from './words';

const toneIcon = { ok: Check, rework: TriangleAlert, muted: Info } as const;

/** Upload, Synced or Live with its icon. */
export function KindLabel({ kind, className }: { kind: KnowledgeSourceKind; className?: string }) {
  const { label, icon: Icon } = sourceKinds[kind];
  return (
    <span className={cn('inline-flex items-center gap-1.5 whitespace-nowrap', className)}>
      <Icon size={15} strokeWidth={1.5} aria-hidden="true" className="shrink-0" />
      {label}
    </span>
  );
}

/** A status's icon and words in its colour; a spinner while it syncs; the reason under it. */
export function StatusText({
  line,
  busy = false,
  className,
}: {
  line: StatusLine;
  busy?: boolean;
  className?: string | undefined;
}) {
  const Icon = busy ? LoaderCircle : toneIcon[line.tone];
  return (
    <span className={cn('flex min-w-0 flex-col gap-0.5', className)}>
      <span className={cn('inline-flex items-start gap-1.5', toneText[line.tone])}>
        <Icon
          size={15}
          strokeWidth={1.75}
          aria-hidden="true"
          className={cn('mt-[3px] shrink-0', busy && 'motion-safe:animate-spin')}
        />
        <span>{line.text}</span>
      </span>
      {line.detail ? (
        <span className="text-[13px] leading-snug [overflow-wrap:anywhere] text-muted">{line.detail}</span>
      ) : null}
    </span>
  );
}

/** The factories whose current version may search it, as chips; "Nothing yet" for none. */
export function UsedBy({ source }: { source: KnowledgeSource }) {
  if (source.used_by.factories.length === 0) return <span className="text-muted">Nothing yet</span>;
  return (
    <span className="flex flex-wrap gap-1">
      {source.used_by.factories.map((f) => (
        <Chip key={f.id} className="border-transparent bg-control">
          <span translate="no">{f.name}</span>
        </Chip>
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

/** The Sync now button of a source, busy while its request is in flight. */
export function SyncNowButton({
  source,
  sync,
  className,
}: {
  source: KnowledgeSource;
  sync: ReturnType<typeof useSyncSource>;
  className?: string;
}) {
  const pending = sync.isPending && sync.variables?.name === source.name;
  return (
    <Button
      variant="white"
      className={className}
      disabled={pending}
      aria-label={`${pending ? 'Asking…' : 'Sync now'}, ${source.title}`}
      onClick={() => sync.mutate(source)}
    >
      {pending ? 'Asking…' : 'Sync now'}
    </Button>
  );
}

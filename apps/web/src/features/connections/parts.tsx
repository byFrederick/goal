// Small pieces the Connections pages share: a connection's or kind's square, its status with an
// icon and words (never colour alone), the kind's mode badges, the write-only mark of a secret
// field and what a run gets.
import { Check, Clock, Info, Lock, TriangleAlert } from 'lucide-react';
import type { Connection, ConnectionKind } from '../../api/types';
import { modeLabels, monogram, statusLine, type Tone, toneText } from '../../shared/connections';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';

const toneIcon = { ok: Check, rework: TriangleAlert, muted: Info } as const;

/** The connection's status: an icon and its words, in the status's colour. */
export function Status({
  connection,
  kind,
  className,
}: {
  connection: Connection;
  kind?: ConnectionKind | undefined;
  className?: string | undefined;
}) {
  const line = statusLine(connection, kind);
  return <ToneText tone={line.tone} text={line.text} className={className} />;
}

export function ToneText({ tone, text, className }: { tone: Tone; text: string; className?: string | undefined }) {
  const Icon = toneIcon[tone];
  return (
    <span className={cn('inline-flex items-start gap-1.5', toneText[tone], className)}>
      <Icon size={15} strokeWidth={1.75} aria-hidden="true" className="mt-[3px] shrink-0" />
      <span>{text}</span>
    </span>
  );
}

/** A 36 px square with two letters: a connection's or a kind's mark. */
export function Monogram({ name, className }: { name: string; className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        'flex size-9 shrink-0 items-center justify-center rounded-control bg-control text-[13px] font-semibold text-ink',
        className,
      )}
    >
      {monogram(name)}
    </span>
  );
}

/** env, CLI, MCP, git: how runs use the kind. */
export function Modes({ kind }: { kind: ConnectionKind | undefined }) {
  const labels = modeLabels(kind);
  if (labels.length === 0) return null;
  return (
    <span className="flex flex-wrap gap-1">
      {labels.map((m) => (
        <Chip key={m} className="font-mono font-normal">
          {m}
        </Chip>
      ))}
    </span>
  );
}

/** What else the kind does for a run: model access, credentials minted per run. */
export function KindNotes({ kind }: { kind: ConnectionKind | undefined }) {
  if (!kind?.model && !kind?.mint) return null;
  return (
    <span className="flex flex-col gap-0.5 text-[13px] text-run">
      {kind.model ? (
        <span className="inline-flex items-center gap-1">
          <Lock size={13} strokeWidth={1.75} aria-hidden="true" />
          Model access
        </span>
      ) : null}
      {kind.mint ? (
        <span className="inline-flex items-center gap-1">
          <Clock size={13} strokeWidth={1.75} aria-hidden="true" />
          Short-lived per run
        </span>
      ) : null}
    </span>
  );
}

/** The mark beside a secret field's label. */
export function WriteOnly() {
  return (
    <Chip tone="quiet" className="font-normal text-muted">
      <Lock size={12} strokeWidth={1.75} aria-hidden="true" />
      Write-only
    </Chip>
  );
}

/** The env names a run gets, as code chips; "None" when it sets none. */
export function EnvNames({ names, className }: { names: string[]; className?: string }) {
  if (names.length === 0) return <span className={cn('text-sm text-muted', className)}>None</span>;
  return (
    <span className={cn('flex flex-wrap gap-1', className)}>
      {names.map((n) => (
        <code
          key={n}
          translate="no"
          className="rounded-chip border border-line bg-surface px-1.5 py-0.5 font-mono text-xs"
        >
          {n}
        </code>
      ))}
    </span>
  );
}

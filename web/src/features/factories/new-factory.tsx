// The ways to start a factory (C §10.7): the New factory card from 768 px and the dashed button on
// a phone. They lead to D's wizard (/o/:org/new/start?from=blank|template|clone|yaml); until D13
// flips newFactoryScreen in app/screens.ts they are marked "Later" and go nowhere.
import { Copy, FileText, LayoutTemplate, type LucideIcon, Plus } from 'lucide-react';
import { laterText, newFactoryScreen } from '../../app/screens';
import { SectionCard } from '../../ui/card';
import { cn } from '../../ui/cn';
import { ListRow } from '../../ui/list-row';

const starts: readonly { id: string; label: string; sub: string; icon: LucideIcon }[] = [
  { id: 'blank', label: 'Blank', sub: 'Inbox, one agent station, done', icon: Plus },
  { id: 'template', label: 'From a template', sub: 'Templates are stored as data', icon: LayoutTemplate },
  { id: 'clone', label: 'Clone a factory', sub: 'Without its items or credentials', icon: Copy },
  { id: 'yaml', label: 'Import YAML', sub: 'Checked against the schema first', icon: FileText },
];

/** Until the wizard exists, how a factory gets here today. */
export function PushHint() {
  return (
    <>
      Until then, push a definition from a working copy with{' '}
      <code className="text-ink">goal factory push &lt;dir&gt;</code>.
    </>
  );
}

export function NewFactoryCard({ className }: { className?: string }) {
  return (
    <SectionCard
      title={newFactoryScreen.label}
      description={
        <>
          Build the line, agents and connections in the browser; nothing goes in code or environment variables. The
          builder arrives later. <PushHint />
        </>
      }
      className={className}
    >
      <ul className="@container m-0 -mt-1 -mb-2 list-none p-0">
        {starts.map((s) => (
          <li key={s.id}>
            <ListRow
              icon={<s.icon size={18} strokeWidth={1.5} />}
              tone="plain"
              title={s.label}
              sub={s.sub}
              end={<span className="text-xs">{laterText}</span>}
            >
              <span aria-disabled="true" className="cursor-default" />
            </ListRow>
          </li>
        ))}
      </ul>
    </SectionCard>
  );
}

/** The phone's dashed "+ New factory". */
export function NewFactoryButton({ className }: { className?: string }) {
  return (
    <button
      type="button"
      aria-disabled="true"
      className={cn(
        'flex min-h-12 cursor-not-allowed items-center justify-center gap-2 rounded-control border border-dashed border-rule-strong bg-transparent text-[15px] font-medium text-ink',
        className,
      )}
    >
      <Plus size={18} strokeWidth={1.5} aria-hidden="true" />
      {newFactoryScreen.label}
      <span className="text-xs font-normal text-muted">{laterText}</span>
    </button>
  );
}

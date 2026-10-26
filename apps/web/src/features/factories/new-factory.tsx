// The ways to start a factory (C §10.7): the New factory card from 768 px, the header's New
// factory and Import YAML, and the dashed button on a phone. Each opens D's wizard on its Start
// step (/o/:org/new/start?from=blank|template|clone|yaml), with that way chosen.
import { Link } from '@tanstack/react-router';
import { Copy, FileText, LayoutTemplate, type LucideIcon, Plus, Upload } from 'lucide-react';
import type { ReactNode } from 'react';
import { newFactoryScreen } from '../../app/screens';
import { useOrg } from '../../shared/org';
import { Button, type ButtonProps } from '../../ui/button';
import { SectionCard } from '../../ui/card';
import { cn } from '../../ui/cn';
import { ListRow } from '../../ui/list-row';

type From = 'blank' | 'template' | 'clone' | 'yaml';

const starts: readonly { id: From; label: string; sub: string; icon: LucideIcon }[] = [
  { id: 'blank', label: 'Blank', sub: 'Build the line yourself', icon: Plus },
  { id: 'template', label: 'From a template', sub: 'Templates are stored as data', icon: LayoutTemplate },
  { id: 'clone', label: 'Clone a factory', sub: 'Without its items or credentials', icon: Copy },
  { id: 'yaml', label: 'Import YAML', sub: 'Checked against the schema first', icon: FileText },
];

/** A link to the wizard's Start step, with a way to start chosen. */
function StartLink({ from, className, children }: { from?: From; className?: string; children?: ReactNode }) {
  const org = useOrg();
  return (
    <Link
      to={newFactoryScreen.to}
      params={{ org: org.slug, step: 'start' }}
      search={from ? { from } : {}}
      className={className}
    >
      {children}
    </Link>
  );
}

export function NewFactoryCard({ className }: { className?: string }) {
  return (
    <SectionCard
      title={newFactoryScreen.label}
      description="Build the line, agents and connections in the browser; nothing goes in code or environment variables."
      className={className}
    >
      <ul className="@container m-0 -mt-1 -mb-2 list-none p-0">
        {starts.map((s) => (
          <li key={s.id}>
            <ListRow icon={<s.icon size={18} strokeWidth={1.5} />} tone="plain" title={s.label} sub={s.sub}>
              <StartLink from={s.id} />
            </ListRow>
          </li>
        ))}
      </ul>
    </SectionCard>
  );
}

/** The header's New factory and Import YAML buttons. */
export function NewFactoryButtons({ variant }: { variant: 'header' | 'empty' }) {
  const size: NonNullable<ButtonProps['size']> = 'md';
  const yaml = (
    <Button asChild variant="white" size={size} key="yaml">
      <StartLink from="yaml">
        <Upload size={16} strokeWidth={1.5} aria-hidden="true" />
        Import YAML
      </StartLink>
    </Button>
  );
  const blank = (
    <Button asChild variant="primary" size={size} key="new">
      <StartLink>
        <Plus size={16} strokeWidth={1.5} aria-hidden="true" />
        {newFactoryScreen.label}
      </StartLink>
    </Button>
  );
  return <>{variant === 'header' ? [yaml, blank] : [blank, yaml]}</>;
}

/** The phone's dashed "+ New factory". */
export function NewFactoryButton({ className }: { className?: string }) {
  return (
    <StartLink
      className={cn(
        'flex min-h-12 items-center justify-center gap-2 rounded-control border border-dashed border-rule-strong bg-transparent text-[15px] font-medium text-ink no-underline transition-colors duration-200 hover:bg-nav-hover',
        className,
      )}
    >
      <Plus size={18} strokeWidth={1.5} aria-hidden="true" />
      {newFactoryScreen.label}
    </StartLink>
  );
}

// The parts of a credential row: its type's icon tile, its factory chips, its expiry with the
// Replace button inside the reminder window, and its row menu (MASTER.md, Components).
import { Cloud, Ellipsis, KeyRound, RefreshCw, SquarePlus, Terminal } from 'lucide-react';
import type { Credential, CredentialType, Factory } from '../../api/types';
import { FactoryBadge } from '../../app/factory-badge';
import { plural } from '../../shared/format';
import { factoryMark } from '../../shared/marks';
import { Button } from '../../ui/button';
import { cn } from '../../ui/cn';
import { Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger } from '../../ui/menu';
import type { Expiry } from './words';

const icons: Record<CredentialType, typeof KeyRound> = {
  api_key: KeyRound,
  oauth_token: RefreshCw,
  ssh_key: Terminal,
  cloud_role: Cloud,
  app_key: SquarePlus,
};

/** The type's icon on its tile; the tile is red while the credential is in its reminder window. */
export function TypeTile({ type, alert }: { type: CredentialType; alert: boolean }) {
  const Icon = icons[type];
  return (
    <span
      aria-hidden="true"
      className={cn(
        'flex size-8 flex-none items-center justify-center rounded-cell',
        alert ? 'bg-rework-tint text-rework-ink' : 'bg-run-tint text-run-ink',
      )}
    >
      <Icon size={16} strokeWidth={1.5} />
    </span>
  );
}

/** The factories a credential's runs may open: a letter badge each, or "All factories". */
export function FactoryChips({ credential, factories }: { credential: Credential; factories: readonly Factory[] }) {
  if (credential.scope.all_factories) return <span className="text-muted">All factories</span>;
  const scoped = factories.filter((f) => credential.scope.factories.includes(f.id));
  return (
    <span className="inline-flex flex-wrap gap-1">
      {scoped.map((f) => (
        <span key={f.id} className="inline-flex items-center">
          <FactoryBadge mark={factoryMark(f, factories)} className="size-5" />
          <span className="sr-only">{f.name}</span>
        </span>
      ))}
    </span>
  );
}

/**
 * The expiry text. Inside the reminder window it is the danger colour and carries the Replace
 * button; the words say it too, so the colour is never the only signal.
 */
export function ExpiryText({ expiry, onReplace }: { expiry: Expiry; onReplace?: (() => void) | undefined }) {
  switch (expiry.state) {
    case 'never':
      return <span className="text-muted">Never expires</span>;
    case 'set':
      return <span className="text-muted">Expires {expiry.date}</span>;
    case 'expired':
      return <span className="font-semibold text-rework-ink">Expired {expiry.date}</span>;
    case 'reminder':
      return (
        <span className="flex flex-wrap items-center gap-2">
          <span className="font-semibold text-rework-ink">
            Expires {expiry.date}, in {plural(expiry.days, 'day')}
          </span>
          {onReplace ? (
            <Button variant="white" size="sm" onClick={onReplace}>
              Replace
            </Button>
          ) : null}
        </span>
      );
  }
}

/** The row's actions: Replace the value and Delete, for those who manage credentials. */
export function RowMenu({
  credential,
  onReplace,
  onDelete,
}: {
  credential: Credential;
  onReplace: () => void;
  onDelete: () => void;
}) {
  return (
    <Menu>
      <MenuTrigger asChild>
        <Button variant="white" size="icon" aria-label={`Actions for ${credential.name}`}>
          <Ellipsis size={16} strokeWidth={1.5} aria-hidden="true" />
        </Button>
      </MenuTrigger>
      <MenuContent side="bottom" align="end" className="w-56">
        <MenuItem onSelect={onReplace}>Replace value…</MenuItem>
        <MenuSeparator />
        <MenuItem onSelect={onDelete} className="text-rework-ink">
          Delete…
        </MenuItem>
      </MenuContent>
    </Menu>
  );
}

// Key created and Token created (canvas "Api-Keys", "Created: shown once"): the secret, in a mono
// field with Copy. Only the dialog's own buttons close it (no Escape, no click outside), since the
// secret is never shown again; it lives in this dialog's props only and is dropped when it closes.
// A key's dialog (docs/design/screens/organization/api-keys.jpg) shows the key's reach and an "I
// copied it" button; a personal token's dialog (account) asks for a tick first and warns that
// whoever has it acts as its owner.
import { Check, Copy } from 'lucide-react';
import { type ReactNode, type RefObject, useId, useRef, useState } from 'react';
import type { CreatedApiKey, CreatedPersonalToken } from '../../api/types';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { Dialog, DialogContent } from '../../ui/dialog';
import { formatDay } from '../format';
import { scopeLabel } from './scopes';

export function CreatedKeyDialog({
  created,
  factoryNames,
  onDone,
}: {
  created: CreatedApiKey | null;
  /** The organization's factory names by id, for the factories the key reaches. */
  factoryNames: ReadonlyMap<string, string>;
  onDone: () => void;
}) {
  return (
    <Dialog open={created !== null}>
      {created ? <KeyShown key={created.key.id} created={created} factoryNames={factoryNames} onDone={onDone} /> : null}
    </Dialog>
  );
}

export function CreatedTokenDialog({
  created,
  orgName,
  onDone,
}: {
  created: CreatedPersonalToken | null;
  orgName: string;
  onDone: () => void;
}) {
  return (
    <Dialog open={created !== null}>
      {created ? (
        <Shown
          key={created.token.id}
          secret={created.secret}
          title="Token created"
          description="Copy your token now. You won’t see it again."
          what="token"
          warning={`Anyone with this token can act as you in ${orgName}. Goal never asks for it.`}
          onDone={onDone}
        />
      ) : null}
    </Dialog>
  );
}

/** Copy the secret, or select it when the clipboard refuses, and say which. */
function useCopySecret(secret: string, secretRef: RefObject<HTMLElement | null>) {
  const [copied, setCopied] = useState<'yes' | 'failed' | null>(null);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(secret);
      setCopied('yes');
    } catch {
      setCopied('failed');
      // Select it so the person can copy it themselves.
      const range = document.createRange();
      if (secretRef.current) {
        range.selectNodeContents(secretRef.current);
        window.getSelection()?.removeAllRanges();
        window.getSelection()?.addRange(range);
      }
    }
  };

  return { copied, copy };
}

function KeyShown({
  created,
  factoryNames,
  onDone,
}: {
  created: CreatedApiKey;
  factoryNames: ReadonlyMap<string, string>;
  onDone: () => void;
}) {
  const { key, secret } = created;
  const secretRef = useRef<HTMLElement>(null);
  const { copied, copy } = useCopySecret(secret, secretRef);
  const factories = key.factories ? key.factories.map((id) => factoryNames.get(id) ?? id).join(', ') : 'All factories';

  return (
    <DialogContent
      title={
        <span className="flex items-center gap-2">
          <span className="grid size-8 shrink-0 place-items-center rounded-control bg-ok-tint text-ok-ink">
            <Check size={16} strokeWidth={1.8} aria-hidden="true" />
          </span>
          {`Key created: ${key.name}`}
        </span>
      }
      dismissible={false}
      initialFocus={secretRef}
    >
      <p className="m-0 text-secondary text-ink-soft">
        Copy it now. Goal stores only a hash, so this is the only time you can see it.
      </p>
      <div className="flex items-center gap-2 rounded-cell border border-ok bg-ok-tint p-2 pl-3">
        <code
          ref={secretRef}
          tabIndex={-1}
          className="min-w-0 flex-1 font-mono text-secondary leading-relaxed break-all text-ink outline-none"
        >
          {secret}
        </code>
        <Button variant="primary" size="sm" className="h-11 md:h-8" onClick={() => void copy()}>
          {copied === 'yes' ? <Check size={14} aria-hidden="true" /> : <Copy size={14} aria-hidden="true" />}
          {copied === 'yes' ? 'Copied' : 'Copy'}
        </Button>
      </div>
      <p aria-live="polite" className="m-0 text-secondary text-muted">
        {copied === 'failed' ? 'Couldn’t reach the clipboard: the key is selected, copy it with your keyboard.' : ''}
      </p>
      <dl className="m-0 grid grid-cols-[96px_minmax(0,1fr)] items-center gap-x-3 gap-y-2 text-secondary">
        <dt className="text-muted">Can</dt>
        <dd className="m-0 flex flex-wrap gap-2">
          {key.scopes.map((s) => (
            <Chip key={s} tone="quiet">
              {scopeLabel(s)}
            </Chip>
          ))}
        </dd>
        <dt className="text-muted">Factories</dt>
        <dd className="m-0">{factories}</dd>
        <dt className="text-muted">Expires</dt>
        <dd className="m-0">{formatDay(key.expires_at)}</dd>
      </dl>
      <div className="flex justify-end">
        <Button variant="primary" size="lg" className="md:h-10 md:text-sm" onClick={onDone}>
          I copied it
        </Button>
      </div>
    </DialogContent>
  );
}

interface ShownProps {
  secret: string;
  title: string;
  description: string;
  /** What the secret is, in "the token is selected". */
  what: 'token';
  warning?: ReactNode;
  onDone: () => void;
}

function Shown({ secret, title, description, what, warning, onDone }: ShownProps) {
  const [stored, setStored] = useState(false);
  const secretRef = useRef<HTMLElement>(null);
  const { copied, copy } = useCopySecret(secret, secretRef);
  const id = useId();

  return (
    <DialogContent title={title} description={description} dismissible={false} initialFocus={secretRef}>
      <div className="flex flex-col gap-2">
        <div className="flex items-start gap-2 rounded-control border border-line bg-card p-3 pl-3">
          <code
            ref={secretRef}
            tabIndex={-1}
            className="min-w-0 flex-1 font-mono text-secondary leading-relaxed break-all outline-none"
          >
            {secret}
          </code>
          <Button variant="white" size="sm" className="h-11 md:h-8" onClick={() => void copy()}>
            {copied === 'yes' ? <Check size={14} aria-hidden="true" /> : <Copy size={14} aria-hidden="true" />}
            {copied === 'yes' ? 'Copied' : 'Copy'}
          </Button>
        </div>
        <p aria-live="polite" className="m-0 text-secondary text-muted">
          {copied === 'failed'
            ? `Couldn’t reach the clipboard: the ${what} is selected, copy it with your keyboard.`
            : ''}
        </p>
      </div>
      {warning ? <Callout tone="rework">{warning}</Callout> : null}
      <label htmlFor={id} className="flex min-h-11 cursor-pointer items-center gap-3 text-body">
        <input
          id={id}
          type="checkbox"
          checked={stored}
          onChange={(e) => setStored(e.target.checked)}
          className="size-5 shrink-0 accent-ink"
        />
        I’ve stored it somewhere safe
      </label>
      <div className="flex justify-end">
        <Button variant="primary" size="lg" className="md:h-10 md:text-sm" disabled={!stored} onClick={onDone}>
          Done
        </Button>
      </div>
    </DialogContent>
  );
}

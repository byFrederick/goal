// A signing secret, shown once (ADR 0026, decision 2): right after a webhook is created or its secret
// rotated. Only Done closes it, since the secret is never shown again. The page keeps the secret in its
// state while the dialog is open and drops it on Done; no query cache or request keeps it.
import { Check, Copy } from 'lucide-react';
import { useRef, useState } from 'react';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Dialog, DialogContent } from '../../ui/dialog';

export interface ShownSecret {
  endpointName: string;
  secret: string;
  /** Why it is shown: a new webhook, or a rotated secret the sender must be given. */
  reason: 'created' | 'rotated';
}

export function SecretDialog({ shown, onDone }: { shown: ShownSecret | null; onDone: () => void }) {
  return (
    <Dialog open={shown !== null}>{shown ? <Shown key={shown.secret} shown={shown} onDone={onDone} /> : null}</Dialog>
  );
}

function Shown({ shown, onDone }: { shown: ShownSecret; onDone: () => void }) {
  const [copied, setCopied] = useState<'yes' | 'failed' | null>(null);
  const secretRef = useRef<HTMLElement>(null);
  const created = shown.reason === 'created';

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(shown.secret);
      setCopied('yes');
    } catch {
      // The clipboard refused: select the secret so the person can copy it themselves.
      setCopied('failed');
      const range = document.createRange();
      if (secretRef.current) {
        range.selectNodeContents(secretRef.current);
        window.getSelection()?.removeAllRanges();
        window.getSelection()?.addRange(range);
      }
    }
  };

  return (
    <DialogContent
      title={created ? 'Webhook created' : 'Secret rotated'}
      description={
        created
          ? `Give this signing secret to the sender of ${shown.endpointName}. You won’t see it again.`
          : `The old secret of ${shown.endpointName} stops working now. Give the sender this one. You won’t see it again.`
      }
      dismissible={false}
      initialFocus={secretRef}
    >
      <div className="flex flex-col gap-2">
        <span className="text-body font-medium">Signing secret</span>
        <div className="flex items-stretch gap-2">
          <code
            ref={secretRef}
            className="min-w-0 flex-1 overflow-x-auto rounded-control border border-rule bg-panel px-3 py-2 font-mono text-secondary break-all text-ink"
          >
            {shown.secret}
          </code>
          <Button variant="secondary" onClick={() => void copy()}>
            {copied === 'yes' ? (
              <Check size={16} strokeWidth={1.5} aria-hidden="true" />
            ) : (
              <Copy size={16} strokeWidth={1.5} aria-hidden="true" />
            )}
            Copy
          </Button>
        </div>
        <p role="status" className="m-0 text-meta text-muted">
          {copied === 'yes' ? 'Copied.' : copied === 'failed' ? 'Copy did not work. The secret is selected.' : ''}
        </p>
      </div>
      <Callout tone="note">
        Goal keeps only a sealed copy. Rotating makes a new one, and the sender stops until it has it.
      </Callout>
      <div className="flex justify-end">
        <Button variant="primary" onClick={onDone}>
          Done
        </Button>
      </div>
    </DialogContent>
  );
}

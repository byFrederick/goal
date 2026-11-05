// A generated secret, shown once (ADR 0040 decision 12): right after a webhook is created with "Generate one".
// Only Done closes it, since the value is never shown again. The page keeps it in its state while the dialog is
// open and drops it on Done; no query cache, mutation result or URL holds it.
import { useRef, useState } from 'react';
import type { WebhookSender } from '../../api/webhooks';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { CopyButton } from '../../ui/copy-button';
import { Dialog, DialogContent } from '../../ui/dialog';
import { senderLabel } from './words';

export interface ShownSecret {
  webhook: string;
  sender: WebhookSender;
  secret: string;
}

export function SecretDialog({ shown, onDone }: { shown: ShownSecret | null; onDone: () => void }) {
  return (
    <Dialog open={shown !== null}>{shown ? <Shown key={shown.webhook} shown={shown} onDone={onDone} /> : null}</Dialog>
  );
}

function Shown({ shown, onDone }: { shown: ShownSecret; onDone: () => void }) {
  const [failed, setFailed] = useState(false);
  const secretRef = useRef<HTMLElement>(null);

  const selectSecret = () => {
    // The clipboard refused: select the secret so the person can copy it themselves.
    setFailed(true);
    const range = document.createRange();
    if (secretRef.current) {
      range.selectNodeContents(secretRef.current);
      window.getSelection()?.removeAllRanges();
      window.getSelection()?.addRange(range);
    }
  };

  return (
    <DialogContent
      title="Webhook created"
      description={`Paste this secret into ${senderLabel(shown.sender)} for ${shown.webhook}. You won’t see it again.`}
      dismissible={false}
      initialFocus={secretRef}
    >
      <div className="flex flex-col gap-2">
        <span className="text-body font-medium">Secret</span>
        <div className="flex items-stretch gap-2">
          <code
            ref={secretRef}
            tabIndex={-1}
            className="min-w-0 flex-1 overflow-x-auto rounded-control border border-rule bg-panel px-3 py-2 font-mono text-secondary break-all text-ink"
          >
            {shown.secret}
          </code>
          <CopyButton
            size="md"
            text={shown.secret}
            aria-label="Copy the secret"
            onClick={() => setFailed(false)}
            onFailed={selectSecret}
          />
        </div>
        <p role="status" className="m-0 text-meta text-muted">
          {failed ? 'Copy did not work. The secret is selected.' : ''}
        </p>
      </div>
      <Callout tone="note">Goal keeps only a sealed copy. To change it later, replace its value in Secrets.</Callout>
      <div className="flex justify-end">
        <Button variant="primary" onClick={onDone}>
          Done
        </Button>
      </div>
    </DialogContent>
  );
}

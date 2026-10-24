// Key created (canvas "Api-Keys", "Created: shown once"): the secret, in a mono field with Copy,
// and Done once the person ticks "I've stored it somewhere safe". Only Done closes it (no Escape,
// no click outside), since the secret is never shown again; it lives in this dialog's props only
// and is dropped when it closes.
import { Check, Copy } from 'lucide-react';
import { useId, useRef, useState } from 'react';
import type { CreatedApiKey } from '../../api/types';
import { Button } from '../../ui/button';
import { Dialog, DialogContent } from '../../ui/dialog';

export function CreatedKeyDialog({ created, onDone }: { created: CreatedApiKey | null; onDone: () => void }) {
  return (
    <Dialog open={created !== null}>
      {created ? <Shown key={created.key.id} created={created} onDone={onDone} /> : null}
    </Dialog>
  );
}

function Shown({ created, onDone }: { created: CreatedApiKey; onDone: () => void }) {
  const [stored, setStored] = useState(false);
  const [copied, setCopied] = useState<'yes' | 'failed' | null>(null);
  const secretRef = useRef<HTMLElement>(null);
  const id = useId();

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(created.secret);
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

  return (
    <DialogContent
      title="Key created"
      description="Copy your key now. You won’t see it again."
      dismissible={false}
      initialFocus={secretRef}
    >
      <div className="flex flex-col gap-1.5">
        <div className="flex items-start gap-2 rounded-control border border-line bg-surface p-2.5 pl-3">
          <code
            ref={secretRef}
            tabIndex={-1}
            className="min-w-0 flex-1 font-mono text-[13px] leading-relaxed break-all outline-none"
          >
            {created.secret}
          </code>
          <Button variant="white" size="sm" className="h-11 md:h-8" onClick={() => void copy()}>
            {copied === 'yes' ? <Check size={14} aria-hidden="true" /> : <Copy size={14} aria-hidden="true" />}
            {copied === 'yes' ? 'Copied' : 'Copy'}
          </Button>
        </div>
        <p aria-live="polite" className="m-0 text-[13px] text-muted">
          {copied === 'failed' ? 'Couldn’t reach the clipboard: the key is selected, copy it with your keyboard.' : ''}
        </p>
      </div>
      <label htmlFor={id} className="flex min-h-11 cursor-pointer items-center gap-3 text-[15px]">
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

// The Definition's two states of a section while the draft loads or fails (the four states of
// every screen: loading and error here; empty and ready in the views). Loading is the app
// background with an sr-only "Loading" and aria-busy, never placeholder shapes (web rule).
import { Button } from '../../../ui/button';

export function DraftLoading() {
  return (
    <div aria-busy="true" className="min-h-64">
      <span className="sr-only">Loading</span>
    </div>
  );
}

export function DraftFailed({ detail, onRetry }: { detail: string; onRetry: () => void }) {
  return (
    <div
      role="alert"
      className="flex max-w-xl flex-col gap-3 rounded-card border border-rework-tint bg-rework-tint p-5"
    >
      <h2 className="m-0 text-heading font-control text-rework-ink">The draft could not load</h2>
      <p className="m-0 text-body text-ink-soft">{detail}</p>
      <div>
        <Button variant="primary" onClick={onRetry}>
          Try again
        </Button>
      </div>
    </div>
  );
}

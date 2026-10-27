// The "Ask the factory to change it" bar (screens build/definition): a request in words that
// becomes an item, reviewed like any change. It is not built yet: the input and Ask render
// disabled (aria-disabled, "Not available yet"), and nothing is sent (ADR 0016, decision 4).
import { Sparkles } from 'lucide-react';
import { Button } from '../../../ui/button';
import { NotAvailable } from '../../../ui/not-available';

export function AskBar() {
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-2 rounded-card border border-rule bg-shell py-1 pr-1 pl-3 focus-within:outline-2 focus-within:outline-run">
      <Sparkles size={16} strokeWidth={1.75} aria-hidden="true" className="shrink-0 text-run" />
      <NotAvailable label="Ask the factory to change it">
        <input
          type="text"
          readOnly
          aria-disabled="true"
          aria-label="Ask the factory to change it"
          placeholder="Ask the factory to change it, for example: give Review a 4th reviewer"
          className="min-w-0 flex-1 basis-64 bg-transparent text-body text-ink outline-none placeholder:text-muted"
        />
      </NotAvailable>
      <span className="shrink-0 text-meta whitespace-nowrap text-muted">Becomes an item, reviewed like any change</span>
      <NotAvailable label="Ask">
        <Button size="sm" variant="primary">
          Ask
        </Button>
      </NotAvailable>
    </div>
  );
}

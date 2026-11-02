// The "Ask the factory to change its definition" bar (screens build/definition, ADR 0021). Not built yet:
// goald does not run draft requests yet (the runner in docs/engineering/architecture.md is not built), so
// the input is disabled and Send stays "Not available yet" (ADR 0016, decision 4). The request flow
// (draft-requests.ts, the request query keys) comes back when the runner lands.
import { ArrowUp } from 'lucide-react';
import { useId } from 'react';
import { Button } from '../../../ui/button';
import { NotAvailable } from '../../../ui/not-available';

export function AskBar() {
  const inputId = useId();
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-2 rounded-control border border-rule bg-card py-1 pr-1 pl-3 transition-[border-color,box-shadow] duration-(--duration-quick) focus-within:border-run">
      <label htmlFor={inputId} className="sr-only">
        Ask the factory to change its definition
      </label>
      <input
        id={inputId}
        type="text"
        disabled
        placeholder="Ask the factory to change it, for example: give Review a 4th reviewer"
        className="min-w-0 flex-1 basis-64 bg-transparent text-body text-ink outline-none placeholder:text-muted disabled:cursor-not-allowed disabled:opacity-60"
      />
      <span className="hidden shrink-0 text-meta whitespace-nowrap text-muted md:inline">Becomes an item, reviewed like any change</span>
      <NotAvailable>
        <Button type="submit" size="icon" variant="ghost" aria-label="Send" className="text-muted">
          <ArrowUp size={16} strokeWidth={1.75} aria-hidden="true" />
        </Button>
      </NotAvailable>
    </div>
  );
}

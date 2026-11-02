import { type ReactNode, useState } from 'react';
import { TopBarSetContext, type TopBarState } from '../app/top-bar';
import { TooltipProvider } from '../ui/tooltip';

/** The top bar's Back link, crumb and actions, shown where a page sets them (its TopBarSlot). */
export function TopBarProbe({ children }: { children: ReactNode }) {
  const [top, setTop] = useState<TopBarState | null>(null);
  return (
    <TooltipProvider>
      <TopBarSetContext.Provider value={setTop}>
        <section aria-label="Top bar">
          {top?.back}
          {top?.crumb}
          {top?.actions}
        </section>
        {children}
      </TopBarSetContext.Provider>
    </TooltipProvider>
  );
}

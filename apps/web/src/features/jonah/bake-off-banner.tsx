// The recommendation banner (MASTER.md "Chips" and the success tone): a green-tint row with a check, the
// headline, the sentence under it and the page's action. It shows only when the bake-off recommends a model
// other than the current one; the page decides that, through recommendationOf.
import { Check } from 'lucide-react';
import type { ReactNode } from 'react';
import type { Recommendation } from './bake-off-format';

export function RecommendationBanner({
  recommendation,
  action,
  children,
}: {
  recommendation: Recommendation;
  /** The button that applies the model, or the link to the draft once it is written. */
  action: ReactNode;
  /** Anything the action reports: a draft conflict, a failure. */
  children?: ReactNode;
}) {
  return (
    <section
      aria-labelledby="bake-off-recommendation"
      className="flex flex-col gap-4 rounded-card bg-ok-tint p-4 md:flex-row md:items-center md:p-5"
    >
      <div className="flex min-w-0 flex-1 items-start gap-4">
        <span aria-hidden="true" className="grid size-10 flex-none place-items-center rounded-full bg-ok text-white">
          <Check size={20} strokeWidth={2.2} />
        </span>
        <div className="flex min-w-0 flex-col gap-1">
          <h2 id="bake-off-recommendation" className="m-0 text-heading text-ink">
            {recommendation.headline}
          </h2>
          <p className="m-0 text-secondary text-ink-soft">{recommendation.sentence}</p>
        </div>
      </div>
      <div className="flex flex-none flex-col items-start gap-2 md:items-end">{action}</div>
      {children ? <div className="w-full">{children}</div> : null}
    </section>
  );
}

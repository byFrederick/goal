import { type ReactNode, useId } from 'react';

/**
 * One section of the settings page (canvas "Org-Settings"): its title and what it covers on the
 * left, the controls on the right; stacked when main is narrower than 720 px.
 */
export function SettingsSection({ title, lede, children }: { title: string; lede: string; children: ReactNode }) {
  const id = useId();
  return (
    <section
      aria-labelledby={id}
      className="grid gap-4 border-t border-rule py-6 first:border-t-0 first:pt-1 @[720px]:grid-cols-[minmax(0,280px)_minmax(0,1fr)] @[720px]:gap-6"
    >
      <div>
        <h2 id={id} className="m-0 text-lg font-medium">
          {title}
        </h2>
        <p className="m-0 mt-1.5 text-sm leading-normal text-muted">{lede}</p>
      </div>
      <div className="flex min-w-0 flex-col gap-2.5">{children}</div>
    </section>
  );
}

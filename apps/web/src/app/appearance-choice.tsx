// Theme: Auto, Light or Dark, inline in the account menu and the phone's account block, or as the
// three cards of the account page (docs/design/screens/account/account.jpg, "Match device", "Light",
// "Dark"). The choice applies at once and is kept per browser (shared/theme.ts). Auto follows the
// system; the stored value for it stays `system`.
import { Contrast } from 'lucide-react';
import type { ThemeChoice } from '../shared/theme';
import { useTheme } from '../shared/theme';
import { cn } from '../ui/cn';
import { SegmentedControl, type SegmentedOption } from '../ui/segmented-control';

const options: readonly SegmentedOption<ThemeChoice>[] = [
  { value: 'system', label: 'Auto' },
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
];

/** The account page's cards: the label is the picture's, and each swatch is a light and a dark half. */
const cards: readonly { value: ThemeChoice; label: string; halves: [string, string] }[] = [
  { value: 'system', label: 'Match device', halves: ['bg-hover', 'bg-andon'] },
  { value: 'light', label: 'Light', halves: ['bg-hover', 'bg-white'] },
  { value: 'dark', label: 'Dark', halves: ['bg-andon', 'bg-andon-raised'] },
];

export function AppearanceChoice({ layout = 'inline' }: { layout?: 'inline' | 'cards' }) {
  const { choice, setChoice } = useTheme();
  if (layout === 'cards') {
    return (
      <fieldset className="m-0 flex min-w-0 flex-wrap gap-1 rounded-card border border-line bg-panel p-2">
        <legend className="sr-only">Theme</legend>
        {cards.map((card) => {
          const on = choice === card.value;
          return (
            <button
              key={card.value}
              type="button"
              aria-pressed={on}
              onClick={() => setChoice(card.value)}
              className={cn(
                'flex min-w-28 flex-col items-center gap-2 rounded-control border border-transparent p-3 text-body text-ink hover:bg-hover',
                on && 'border-line bg-card',
              )}
            >
              <span aria-hidden="true" className="flex h-12 w-full overflow-hidden rounded-chip border border-line">
                <span className={cn('flex-1', card.halves[0])} />
                <span className={cn('flex-1', card.halves[1])} />
              </span>
              {card.label}
            </button>
          );
        })}
      </fieldset>
    );
  }
  return (
    <div className="flex items-center gap-3 rounded-control px-2 py-1 text-body text-ink">
      <Contrast size={16} strokeWidth={1.5} aria-hidden="true" className="shrink-0 text-muted" />
      <span className="flex-1">Theme</span>
      <SegmentedControl label="Theme" options={options} value={choice} onChange={setChoice} />
    </div>
  );
}

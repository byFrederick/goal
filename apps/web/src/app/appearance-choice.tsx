// Theme: Auto, Light or Dark, inline in the account menu and the phone's account block. The choice
// applies at once and is kept per browser (shared/theme.ts). Auto follows the system; the stored
// value for it stays `system`.
import { Contrast } from 'lucide-react';
import type { ThemeChoice } from '../shared/theme';
import { useTheme } from '../shared/theme';
import { SegmentedControl, type SegmentedOption } from '../ui/segmented-control';

const options: readonly SegmentedOption<ThemeChoice>[] = [
  { value: 'system', label: 'Auto' },
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
];

export function AppearanceChoice() {
  const { choice, setChoice } = useTheme();
  return (
    <div className="flex items-center gap-3 rounded-control px-2 py-1 text-body text-ink">
      <Contrast size={16} strokeWidth={1.5} aria-hidden="true" className="shrink-0 text-muted" />
      <span className="flex-1">Theme</span>
      <SegmentedControl label="Theme" options={options} value={choice} onChange={setChoice} />
    </div>
  );
}

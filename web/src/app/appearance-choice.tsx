// Appearance: System, Light or Dark, in the account menu and the phone's account block. The
// choice applies at once and is kept per browser (shared/theme.ts).
import type { ThemeChoice } from '../shared/theme';
import { useTheme } from '../shared/theme';
import { SegmentedControl, type SegmentedOption } from '../ui/segmented-control';

const options: readonly SegmentedOption<ThemeChoice>[] = [
  { value: 'system', label: 'System' },
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
];

export function AppearanceChoice() {
  const { choice, setChoice } = useTheme();
  return (
    <div className="flex flex-col gap-1.5 px-2 py-1">
      <span className="text-[13px] text-muted">Appearance</span>
      <SegmentedControl label="Appearance" options={options} value={choice} onChange={setChoice} />
    </div>
  );
}

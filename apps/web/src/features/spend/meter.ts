// The ARIA meter of a styled track: a native meter cannot carry the fill's colour, so the bars of this
// screen set the role by hand (as ui/target-bar.tsx does).
export const meterOf = (label: string, value: number, max: number) => ({
  role: 'meter',
  'aria-label': label,
  'aria-valuemin': 0,
  'aria-valuemax': max,
  'aria-valuenow': value,
});

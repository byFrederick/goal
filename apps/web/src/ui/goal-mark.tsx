// The Goal mark: a cobalt square holding a white G that ends in a yellow node (the brand button in
// docs/design/screens/account/shell.html, and sign-up.html). Decorative: the link or button around it
// holds the name "Goal", so the square is hidden from assistive technology.
import { cn } from './cn';

/**
 * `shell` is the sidebar's square (shell.html's 26 px, snapped to the 4 px scale: 28 px, 16 px glyph);
 * `brand` is the sign-up page's (sign-up.html's 30 px, snapped to 32 px, 20 px glyph);
 * `hero` is the phone sign-in's (mobile/sign-in.html's 56 px square, 32 px glyph).
 */
const sizes = {
  shell: { square: 'size-7', glyph: 'size-4' },
  brand: { square: 'size-8', glyph: 'size-5' },
  hero: { square: 'size-14', glyph: 'size-8' },
} as const;

interface GoalMarkProps {
  size?: keyof typeof sizes;
  className?: string;
}

export function GoalMark({ size = 'shell', className }: GoalMarkProps) {
  const { square, glyph } = sizes[size];
  return (
    <span
      aria-hidden="true"
      className={cn('inline-grid flex-none place-items-center rounded-control bg-run', square, className)}
    >
      <svg className={glyph} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
        <path
          d="M18.6 6.9A8.2 8.2 0 1 0 20.2 12H13"
          fill="none"
          className="stroke-white"
          strokeWidth={3.2}
          strokeLinecap="round"
        />
        <circle cx={12.4} cy={12} r={2.9} className="fill-amber" />
      </svg>
    </span>
  );
}

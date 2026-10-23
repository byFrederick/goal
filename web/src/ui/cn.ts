import { type ClassValue, clsx } from 'clsx';
import { extendTailwindMerge } from 'tailwind-merge';

// tailwind-merge must know the custom radius names, or it drops them against rounded-*.
const merge = extendTailwindMerge({
  extend: {
    theme: { radius: ['shell', 'card', 'control', 'chip'], tracking: ['display', 'numeral'] },
  },
});

export function cn(...inputs: ClassValue[]): string {
  return merge(clsx(inputs));
}

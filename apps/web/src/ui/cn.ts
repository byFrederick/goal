import { type ClassValue, clsx } from 'clsx';
import { extendTailwindMerge } from 'tailwind-merge';

// tailwind-merge must know the custom names, or it drops them against their utility family: a text
// role (text-body) would be read as a colour and drop text-white, and font-control as a family.
const merge = extendTailwindMerge({
  extend: {
    theme: {
      radius: ['card', 'control', 'chip'],
      tracking: ['display', 'numeral'],
      text: ['meta', 'secondary', 'body', 'heading', 'detail-title', 'page-title', 'kpi'],
      'font-weight': ['control'],
    },
  },
});

export function cn(...inputs: ClassValue[]): string {
  return merge(clsx(inputs));
}

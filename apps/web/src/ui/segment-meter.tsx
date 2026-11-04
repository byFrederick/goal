import { useLayoutEffect, useRef, useState } from 'react';
import { cn } from './cn';

/** A segment narrower than this many px gives way to one continuous bar. */
export const MIN_SEGMENT_PX = 4;
/** The width assumed until the container has been measured (and where nothing can be measured). */
const FALLBACK_WIDTH_PX = 160;

export type SegmentTones = {
  /** Classes of a filled segment (and of the bar's fill). */
  filled: string;
  /** Classes of a waiting segment (and of the bar's second fill). */
  waiting?: string;
  /** Classes of a free segment (and of the bar's track unless `track` is given). */
  empty: string;
};

/** Whether `total` segments of at most `maxWidth` px, `gap` px apart, fit `width` px as segments or as one bar. */
export function meterLayout(total: number, width: number, gap: number): 'segments' | 'bar' {
  if (total <= 1) return 'segments';
  return (width - gap * (total - 1)) / total < MIN_SEGMENT_PX ? 'bar' : 'segments';
}

/** The width `total` segments of `segmentWidth` px take with their gaps. */
export function naturalWidth(total: number, segmentWidth: number, gap: number): number {
  return Math.max(0, total * segmentWidth + gap * (total - 1));
}

/**
 * One mark per unit (a part, a slot), `filled` of `total` of them done and `waiting` more waiting on a person.
 * It never draws wider than its container: segments share the width and each stays at most `segmentWidth`
 * wide (no maximum: they fill the row). When a segment would be narrower than 4 px it draws one continuous
 * bar, filled to filled / total, instead. With `label` it is an image named by it; without, it is decorative.
 */
export function SegmentMeter({
  total,
  filled,
  waiting = 0,
  segmentWidth,
  height,
  gap,
  tones,
  track,
  segmentClassName,
  label,
  testId,
  className,
}: {
  total: number;
  filled: number;
  waiting?: number;
  /** Each segment's maximum width in px; without it the meter takes the whole row. */
  segmentWidth?: number;
  height: number;
  gap: number;
  tones: SegmentTones;
  /** Classes of the bar's track; defaults to the empty tone. */
  track?: string;
  segmentClassName?: string;
  label?: string;
  testId?: string;
  className?: string;
}) {
  const units = Math.max(0, Math.floor(total));
  const done = Math.min(Math.max(0, filled), units);
  const wait = Math.min(Math.max(0, waiting), units - done);
  const ref = useRef<HTMLSpanElement>(null);
  const [width, setWidth] = useState<number | null>(null);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const measure = () => {
      const w = Math.round(el.getBoundingClientRect().width);
      if (w > 0) setWidth((prev) => (prev === w ? prev : w));
    };
    measure();
    if (typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  const natural = segmentWidth === undefined ? undefined : naturalWidth(units, segmentWidth, gap);
  // Unmeasured, judge by the fallback width, capped at the natural width when there is one.
  const available = width ?? Math.min(FALLBACK_WIDTH_PX, natural ?? FALLBACK_WIDTH_PX);
  const bar = meterLayout(units, available, gap) === 'bar';
  const a11y = label ? { role: 'img', 'aria-label': label } : { 'aria-hidden': true as const };

  return (
    <span
      ref={ref}
      {...a11y}
      data-testid={testId}
      data-mode={bar ? 'bar' : 'segments'}
      className={cn('block min-w-0 max-w-full', className)}
      style={{ width: natural === undefined ? '100%' : `${natural}px`, height }}
    >
      {bar ? (
        <span className={cn('relative block h-full overflow-hidden rounded-chip', track ?? tones.empty)}>
          <span
            className={cn(
              'absolute inset-0 origin-left rounded-chip transition-transform duration-(--duration-fast) ease-out motion-reduce:transition-none',
              tones.filled,
            )}
            style={{ transform: `scaleX(${units > 0 ? done / units : 0})` }}
          />
          {wait > 0 ? (
            <span
              className={cn(
                'absolute inset-0 origin-left rounded-chip transition-transform duration-(--duration-fast) ease-out motion-reduce:transition-none',
                tones.waiting ?? tones.filled,
              )}
              style={{ transform: `translateX(${(done / units) * 100}%) scaleX(${wait / units})` }}
            />
          ) : null}
        </span>
      ) : (
        <span className="flex h-full" style={{ gap }}>
          {Array.from({ length: units }, (_, i) => (
            <i
              // biome-ignore lint/suspicious/noArrayIndexKey: the segments are fixed slots by position, never reordered
              key={i}
              data-done={i < done}
              className={cn(
                'block h-full min-w-0 flex-1 rounded-chip transition-colors duration-(--duration-quick) ease-out motion-reduce:transition-none',
                segmentClassName,
                i < done ? tones.filled : i < done + wait ? (tones.waiting ?? tones.filled) : tones.empty,
              )}
              style={segmentWidth === undefined ? undefined : { maxWidth: segmentWidth }}
            />
          ))}
        </span>
      )}
    </span>
  );
}

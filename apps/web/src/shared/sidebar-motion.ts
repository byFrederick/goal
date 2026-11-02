// How the sidebar's collapse, expand and snap to the rail move (MOTION.md, "Sidebar collapse / expand and the
// snap to the rail"). Only a click or a drag release animates; a key never does (rule 1), and nothing animates
// while the handle is dragged. The width itself changes at once: the inner wrapper starts scaled to the width
// it had and eases to none (a FLIP transform), and the labels fade.

/** The widths the column had and has, and whether the person asked for reduced motion (only the fade then). */
export interface SidebarMotion {
  from: number;
  to: number;
  reduced: boolean;
}

/** Whether the person asked for reduced motion; a browser without matchMedia (jsdom) allows motion. */
export function reducedMotion(): boolean {
  return typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
}

/** The motion for a change of the shown width, or null when the width stays or the change is not a collapse or expand. */
export function sidebarMotion(from: number, to: number): SidebarMotion | null {
  return from === to ? null : { from, to, reduced: reducedMotion() };
}

/** The wrapper's starting scale: the width it had over the width it now fills. */
export function startScale(motion: SidebarMotion): number {
  return motion.from / motion.to;
}

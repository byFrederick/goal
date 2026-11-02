// The "On this page" links of the settings screen. The link of the section in view is marked
// (aria-current="location", the hover fill, MASTER.md and the design's `.toc a.on`); the mark follows
// scrolling and clicks. It changes colour only: a selection that follows scrolling is high frequency,
// so nothing slides (MOTION.md, Hover on anything).
import { useEffect, useState } from 'react';
import { cn } from '../../ui/cn';

export interface SectionLink {
  id: string;
  label: string;
}

export function SectionNav({ links }: { links: readonly SectionLink[] }) {
  const [current, setCurrent] = useState(links[0]?.id);
  const ids = links.map((l) => l.id).join(',');

  useEffect(() => {
    if (typeof IntersectionObserver === 'undefined') return;
    const visible = new Set<string>();
    const order = ids.split(',');
    const observer = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (e.isIntersecting) visible.add(e.target.id);
          else visible.delete(e.target.id);
        }
        const first = order.find((id) => visible.has(id));
        if (first) setCurrent(first);
      },
      { rootMargin: '0px 0px -60% 0px' },
    );
    for (const id of order) {
      const el = document.getElementById(id);
      if (el) observer.observe(el);
    }
    return () => observer.disconnect();
  }, [ids]);

  return (
    <nav aria-label="On this page" className="hidden xl:sticky xl:top-5 xl:flex xl:flex-col xl:gap-1 text-secondary">
      {links.map((l) => (
        <a
          key={l.id}
          href={`#${l.id}`}
          aria-current={current === l.id ? 'location' : undefined}
          onClick={() => setCurrent(l.id)}
          className={cn(
            'press rounded-control px-3 py-1 text-ink-soft no-underline transition-colors duration-(--duration-quick) ease-(--ease-out) hover:bg-hover hover:text-ink',
            'aria-[current=location]:bg-hover aria-[current=location]:font-medium aria-[current=location]:text-ink',
          )}
        >
          {l.label}
        </a>
      ))}
    </nav>
  );
}

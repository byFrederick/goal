import { cn } from './cn';

/** The mark: three stations, the middle one the constraint. */
export function LogoMark({ className }: { className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        'flex size-[30px] shrink-0 items-end justify-center gap-[3px] rounded-control bg-ink pb-[7px]',
        className,
      )}
    >
      <span className="h-[7px] w-1 rounded-[1px] bg-white" />
      <span className="h-3 w-1 rounded-[1px] bg-amber" />
      <span className="h-[17px] w-1 rounded-[1px] bg-white" />
    </span>
  );
}

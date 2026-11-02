// A factory's letter badge (MASTER.md "Colour" and "Sidebar"): its mark on the run colour, always 18 px.
export function FactoryBadge({ mark }: { mark: string }) {
  return (
    <span
      aria-hidden="true"
      className="flex size-[18px] flex-none items-center justify-center rounded-chip bg-run text-meta font-semibold text-white"
    >
      {mark}
    </span>
  );
}

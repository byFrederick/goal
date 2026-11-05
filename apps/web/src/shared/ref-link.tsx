// A link to an item by its ref (<factory name>#<number>), another factory's included: the factory is the
// ref's own, so it opens the item in the factory that owns it.
import { Link } from '@tanstack/react-router';
import { refParam } from './factory';
import { useOrg } from './org';

/** The factory a ref names: everything before its last "#". */
export function factoryOfRef(ref: string): string {
  return ref.slice(0, ref.lastIndexOf('#'));
}

export function RefLink({ item, className }: { item: string; className?: string | undefined }) {
  const org = useOrg();
  return (
    <Link
      to="/o/$org/f/$factory/items/$ref"
      params={{ org: org.slug, factory: factoryOfRef(item), ref: refParam(item) }}
      className={className ?? 'font-medium text-link no-underline hover:underline'}
    >
      {item}
    </Link>
  );
}

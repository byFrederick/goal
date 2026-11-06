// An item's repositories on a Board card and an Items row (ADR 0041, decision 11): the first one in
// mono, "+N" for the rest, and an ⓘ listing every one with how it joined. They show only on a board
// whose items name more than one repository, so a one-repository line stays as it was.
import type { ItemCard, ItemRepository } from '../api/types';
import { InfoTip } from '../ui/info-tip';

/** How a repository joined the item, in the words of the ⓘ. */
export const joinedWords: Record<ItemRepository['joined'], string> = {
  filing: 'Named when filed',
  default: 'Factory default',
  push: 'An agent pushed to it',
  output: 'A station’s output named it',
};

/** The repository as "owner/name": the host is the same for nearly every item and costs the width. */
export function repoName(repo: string): string {
  const parts = repo.split('/');
  return parts.length > 2 ? parts.slice(1).join('/') : repo;
}

/** The loaded cards name more than one distinct repository: the factory has several in use. */
export function usesSeveralRepositories(items: readonly Pick<ItemCard, 'repositories'>[]): boolean {
  const seen = new Set<string>();
  for (const item of items) for (const r of item.repositories ?? []) seen.add(r.repo);
  return seen.size > 1;
}

/** Nothing for an item with no repository. */
export function ItemRepositories({
  item,
  className,
}: {
  item: Pick<ItemCard, 'repositories' | 'ref'>;
  className?: string | undefined;
}) {
  const repositories = item.repositories ?? [];
  const first = repositories[0];
  if (!first) return null;
  const more = repositories.length - 1;
  const full = repositories.map((r) => `${repoName(r.repo)}: ${joinedWords[r.joined]}`).join('. ');
  return (
    <span className={`relative z-10 flex min-w-0 items-center gap-1 text-meta text-muted ${className ?? ''}`}>
      <span className="min-w-0 truncate font-mono" title={repositories.map((r) => repoName(r.repo)).join(', ')}>
        {repoName(first.repo)}
      </span>
      {more > 0 ? <span className="shrink-0 tabular-nums">+{more}</span> : null}
      <InfoTip about={`${item.ref} repositories`} className="-my-1">
        {full}
      </InfoTip>
    </span>
  );
}

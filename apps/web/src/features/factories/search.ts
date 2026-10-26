// The Factories home's search params: ?archived=true shows the Archived filter's list. Kept in the
// URL, so Back returns to the filter the person left.
export interface FactoriesSearch {
  archived?: true;
}

export function factoriesSearch(search: Record<string, unknown>): FactoriesSearch {
  return search.archived === true || search.archived === 'true' ? { archived: true } : {};
}

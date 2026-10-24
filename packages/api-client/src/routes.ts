// Where an organization's routes live. Every route except /v1/health belongs to an organization.
// Today goald serves one implicit organization directly under /v1; phase 2.2 A5 moves the routes
// under /v1/orgs/{org} (A §8.3). That move changes `orgRoutes` here, and the compiler then names
// every call that still lacks the `org` path parameter. Nothing else spells the prefix.

/** The prefix of every organization-owned route. */
export const orgRoutes = "/v1";

/** The contract path of an organization-owned route, e.g. OrgPath<"/factories"> = "/v1/factories". */
export type OrgPath<Route extends `/${string}`> = `${typeof orgRoutes}${Route}`;

/**
 * orgPath turns a route relative to the organization ("/factories/{factory}/board") into its
 * contract path, typed as the literal key of `paths` so openapi-fetch checks its parameters.
 */
export function orgPath<Route extends `/${string}`>(route: Route): OrgPath<Route> {
  return `${orgRoutes}${route}`;
}

/**
 * routeUrl builds the URL of a contract path for clients openapi-fetch does not cover
 * (EventSource): each param fills its `{name}` placeholder, percent-encoded, and the rest become
 * the query string. Undefined and empty values are left out.
 */
export function routeUrl(baseUrl: string, path: string, params: Readonly<Record<string, string | undefined>>): string {
  const query = new URLSearchParams();
  let filled = path;

  for (const [name, value] of Object.entries(params)) {
    if (value === undefined || value === "") continue;
    const placeholder = `{${name}}`;
    if (filled.includes(placeholder)) filled = filled.replaceAll(placeholder, encodeURIComponent(value));
    else query.set(name, value);
  }

  const qs = query.size > 0 ? `?${query.toString()}` : "";
  return `${baseUrl.replace(/\/+$/, "")}${filled}${qs}`;
}

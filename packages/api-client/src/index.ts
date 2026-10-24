// Typed client for goald's HTTP API. The types in ./schema.ts are generated from
// api/openapi.yaml (`task api:gen`); never edit them by hand.
//
//   const goal = createGoalClient("http://127.0.0.1:8080");
//   const board = await unwrap(
//     goal.GET(orgPath("/factories/{factory}/board"), { params: { path: { org: "acme", factory: "goal" } } }),
//   );
//
// Organization-owned routes (everything but /v1/health and /v1/orgs) are written relative to the
// organization through orgPath and take its slug or id as the `org` path parameter. Items are
// addressed by id or by reference ("goal#2"); path parameters are percent-encoded, so a
// reference can be passed as is.
import createClient, { type ClientOptions } from "openapi-fetch";
import type { components, paths } from "./schema";

export { ApiError, type Problem, type Result, unwrap } from "./problem";
export { type OrgPath, orgPath } from "./routes";
export type { components, operations, paths } from "./schema";
export { type StreamEvent, type StreamHandlers, type StreamScope, type StreamStatus, subscribe } from "./stream";

type Schemas = components["schemas"];

export type Factory = Schemas["Factory"];
export type FactoryList = Schemas["FactoryList"];
export type Station = Schemas["Station"];
export type StationKind = Schemas["StationKind"];
export type StationAction = Schemas["StationAction"];
export type Item = Schemas["Item"];
export type ItemCard = Schemas["ItemCard"];
export type ItemPage = Schemas["ItemPage"];
export type ItemState = Schemas["ItemState"];
export type ClassOfService = Schemas["ClassOfService"];
export type NewItem = Schemas["NewItem"];
export type MergeRequest = Schemas["MergeRequest"];
export type Rejection = Schemas["Rejection"];
export type Org = Schemas["Org"];
export type OrgSummary = Schemas["OrgSummary"];
export type OrgList = Schemas["OrgList"];
export type NewOrg = Schemas["NewOrg"];
export type Actor = Schemas["Actor"];
export type Board = Schemas["Board"];
export type BoardStation = Schemas["BoardStation"];
export type Metrics = Schemas["Metrics"];
export type ItemEvent = Schemas["Event"];
export type EventPage = Schemas["EventPage"];
export type AndonPull = Schemas["AndonPull"];
export type OrgAttention = Schemas["OrgAttention"];
export type FactoryAttention = Schemas["FactoryAttention"];
export type FactoryOverviewList = Schemas["FactoryOverviewList"];
export type FactoryOverview = Schemas["FactoryOverview"];
export type FactoryHealth = Schemas["FactoryHealth"];
export type Inbox = Schemas["Inbox"];
export type InboxEntry = Schemas["InboxEntry"];
export type InboxEntryKind = Schemas["InboxEntryKind"];

/** createGoalClient returns a typed fetch client for goald at baseUrl ("" for the same origin). */
export function createGoalClient(baseUrl: string, options: Omit<ClientOptions, "baseUrl"> = {}) {
  return createClient<paths>({ ...options, baseUrl });
}

export type GoalClient = ReturnType<typeof createGoalClient>;

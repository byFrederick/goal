// Typed client for goald's HTTP API. The types in ./schema.gen.ts are generated from
// contracts/api/openapi.yaml (`task api:gen`); never edit them by hand.
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
import type { components, paths } from "./schema.gen";

export { ApiError, type Problem, type Result, unwrap } from "./problem";
export { type OrgPath, orgPath } from "./routes";
export type { components, operations, paths } from "./schema.gen";
export {
  type FactoryEvent,
  type StreamEvent,
  type StreamHandlers,
  type StreamScope,
  type StreamStatus,
  subscribe,
} from "./stream";

type Schemas = components["schemas"];

export type Factory = Schemas["Factory"];
export type FactoryList = Schemas["FactoryList"];
export type Station = Schemas["Station"];
export type StationKind = Schemas["StationKind"];
export type StationAction = Schemas["StationAction"];
export type Item = Schemas["Item"];
export type AssemblyParts = Schemas["AssemblyParts"];
export type AssemblyPart = Schemas["AssemblyPart"];
export type NewPartRequest = Schemas["NewPartRequest"];
export type PartOf = Schemas["PartOf"];
export type PartsSummary = Schemas["PartsSummary"];
export type ItemCard = Schemas["ItemCard"];
export type ItemFlow = Schemas["ItemFlow"];
export type ItemSpan = Schemas["ItemSpan"];
export type ItemWaiting = Schemas["ItemWaiting"];
export type ItemQuestion = Schemas["ItemQuestion"];
export type ItemAnswer = Schemas["ItemAnswer"];
export type ItemBudget = Schemas["ItemBudget"];
export type CurrentRun = Schemas["CurrentRun"];
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
export type Activity = Schemas["Activity"];
export type ActivityEvent = Schemas["ActivityEvent"];
export type OrgSettings = Schemas["OrgSettings"];
export type OrgSettingsPatch = Schemas["OrgSettingsPatch"];
export type OrgSlots = Schemas["OrgSlots"];
export type FactorySlots = Schemas["FactorySlots"];
export type OrgDeleted = Schemas["OrgDeleted"];
export type AuditEntry = Schemas["AuditEntry"];
export type AuditPage = Schemas["AuditPage"];

// Identity (spec 3): how the install signs people in and who a request acts for.
export type AuthConfig = Schemas["AuthConfig"];
export type AuthMode = Schemas["AuthMode"];
export type SignInMethod = Schemas["SignInMethod"];
export type Me = Schemas["Me"];
export type DeviceRequest = Schemas["DeviceRequest"];
export type User = Schemas["User"];
export type OrgRole = Schemas["OrgRole"];
export type FactoryRole = Schemas["FactoryRole"];
export type Permission = Schemas["Permission"];
export type ConnectionKindList = Schemas["ConnectionKindList"];
export type ConnectionKind = Schemas["ConnectionKind"];
export type ConnectionKindField = Schemas["ConnectionKindField"];
export type ConnectionKindFile = Schemas["ConnectionKindFile"];
export type ConnectionList = Schemas["ConnectionList"];
export type Connection = Schemas["Connection"];
export type ConnectionUsers = Schemas["ConnectionUsers"];
export type NewConnection = Schemas["NewConnection"];
export type ConnectionPatch = Schemas["ConnectionPatch"];
export type ConnectionTestResult = Schemas["ConnectionTestResult"];
export type ConnectionEvent = Schemas["ConnectionEvent"];
export type Credential = Schemas["Credential"];
export type CredentialType = Schemas["CredentialType"];
export type CredentialUsers = Schemas["CredentialUsers"];
export type CredentialPatch = Schemas["CredentialPatch"];
export type KnowledgeSourceList = Schemas["KnowledgeSourceList"];
export type KnowledgeSource = Schemas["KnowledgeSource"];
export type KnowledgeSourceKind = Schemas["KnowledgeSourceKind"];
export type NewKnowledgeSource = Schemas["NewKnowledgeSource"];
export type KnowledgeSourcePatch = Schemas["KnowledgeSourcePatch"];
export type KnowledgeDocument = Schemas["KnowledgeDocument"];
export type KnowledgeDocumentList = Schemas["KnowledgeDocumentList"];
export type KnowledgeDocumentPage = Schemas["KnowledgeDocumentPage"];
export type KnowledgeDocumentText = Schemas["KnowledgeDocumentText"];
export type KnowledgeSearchResult = Schemas["KnowledgeSearchResult"];
export type KnowledgeHit = Schemas["KnowledgeHit"];
export type KnowledgeEvent = Schemas["KnowledgeEvent"];
export type ApiKey = Schemas["ApiKey"];
export type NewApiKey = Schemas["NewApiKey"];
export type CreatedApiKey = Schemas["CreatedApiKey"];
export type Member = Schemas["Member"];
export type FactoryRoleGrant = Schemas["FactoryRoleGrant"];
export type Invitation = Schemas["Invitation"];
export type TransferRequest = Schemas["TransferRequest"];
export type PersonalToken = Schemas["PersonalToken"];
export type NewPersonalToken = Schemas["NewPersonalToken"];
export type CreatedPersonalToken = Schemas["CreatedPersonalToken"];

// Definitions (B §8): factories' drafts, versions, validation and templates.
export type VersionRef = Schemas["VersionRef"];
export type DraftRef = Schemas["DraftRef"];
export type NewFactoryRequest = Schemas["NewFactoryRequest"];
export type FactoryStart = Schemas["FactoryStart"];
export type FileMap = Schemas["FileMap"];
export type FileContent = Schemas["FileContent"];
export type FileInfo = Schemas["FileInfo"];
export type IntakeEvent = Schemas["IntakeEvent"];
export type IntakeTriggerDetail = Schemas["IntakeTriggerDetail"];
export type FileList = Schemas["FileList"];
export type Section = Schemas["Section"];
export type LineRange = Schemas["LineRange"];
export type Ranges = Schemas["Ranges"];
export type SectionSummary = Schemas["SectionSummary"];
export type VersionState = Schemas["VersionState"];
export type VersionSource = Schemas["VersionSource"];
export type Draft = Schemas["Draft"];
export type OpenDraftRequest = Schemas["OpenDraftRequest"];
export type SectionETags = Schemas["SectionETags"];
export type DraftModel = Schemas["DraftModel"];
export type FactoryModel = Schemas["FactoryModel"];
export type VersionModel = Schemas["VersionModel"];
export type SectionSave = Schemas["SectionSave"];
export type SaveResult = Schemas["SaveResult"];
export type DraftTree = Schemas["DraftTree"];
export type StationMoves = Schemas["StationMoves"];
export type ValidateDraftRequest = Schemas["ValidateDraftRequest"];
export type PublishRequest = Schemas["PublishRequest"];
export type RestoreRequest = Schemas["RestoreRequest"];
export type DefinitionCounts = Schemas["DefinitionCounts"];
export type CheckResult = Schemas["CheckResult"];
export type Validation = Schemas["Validation"];
export type DefinitionProblem = Schemas["DefinitionProblem"];
export type ProblemFix = Schemas["ProblemFix"];
export type PatchOp = Schemas["PatchOp"];
export type ParseError = Schemas["ParseError"];
export type ItemsAtRemovedStations = Schemas["ItemsAtRemovedStations"];
export type ItemAt = Schemas["ItemAt"];
export type Version = Schemas["Version"];
export type VersionDetail = Schemas["VersionDetail"];
export type VersionPage = Schemas["VersionPage"];
export type CompareSide = Schemas["CompareSide"];
export type Comparison = Schemas["Comparison"];
export type FileChange = Schemas["FileChange"];
export type Change = Schemas["Change"];
export type TemplateSummary = Schemas["TemplateSummary"];
export type Template = Schemas["Template"];
export type DefinitionEvent = Schemas["DefinitionEvent"];
// Builder (D §8): editor metadata and the skills.sh skills catalog.
export type BuilderMeta = Schemas["BuilderMeta"];
export type BuilderModel = Schemas["BuilderModel"];
export type BuilderCapabilities = Schemas["BuilderCapabilities"];
export type BuilderLimits = Schemas["BuilderLimits"];
export type SkillListing = Schemas["SkillListing"];
export type SkillPage = Schemas["SkillPage"];
export type SkillFiles = Schemas["SkillFiles"];
export type TextFile = Schemas["TextFile"];
export type Base64File = Schemas["Base64File"];
export type Board = Schemas["Board"];
export type BoardLane = Schemas["BoardLane"];
export type BoardLine = Schemas["BoardLine"];
export type BoardStation = Schemas["BoardStation"];
export type Metrics = Schemas["Metrics"];
export type MetricsSummary = Schemas["MetricsSummary"];
export type Targets = Schemas["Targets"];
export type WeeklyThroughput = Schemas["WeeklyThroughput"];
export type LeadTimeBridge = Schemas["LeadTimeBridge"];
export type BridgeStep = Schemas["BridgeStep"];
export type ConstraintReport = Schemas["ConstraintReport"];
export type ConstraintCandidate = Schemas["ConstraintCandidate"];
export type ConstraintSource = Schemas["ConstraintSource"];
export type AgentStats = Schemas["AgentStats"];
export type EvalList = Schemas["EvalList"];
export type EvalRun = Schemas["EvalRun"];
export type EvalSummary = Schemas["EvalSummary"];
export type EvalCaseEntry = Schemas["EvalCaseEntry"];
export type EvalCheckedBy = Schemas["EvalCheckedBy"];
export type EvalHistoryTry = Schemas["EvalHistoryTry"];
export type EvalResultState = Schemas["EvalResultState"];
export type EvalRunRequest = Schemas["EvalRunRequest"];
export type EvalRunStarted = Schemas["EvalRunStarted"];
export type EvalEstimate = Schemas["EvalEstimate"];
export type EvalCaseFromRun = Schemas["EvalCaseFromRun"];
export type BakeOff = Schemas["BakeOff"];
export type BakeOffArm = Schemas["BakeOffArm"];
export type BakeOffArmSummary = Schemas["BakeOffArmSummary"];
export type BakeOffCase = Schemas["BakeOffCase"];
export type BakeOffCaseArm = Schemas["BakeOffCaseArm"];
export type BakeOffJudged = Schemas["BakeOffJudged"];
export type BakeOffList = Schemas["BakeOffList"];
export type BakeOffStarted = Schemas["BakeOffStarted"];
export type BakeOffApply = Schemas["BakeOffApply"];
export type Intake = Schemas["Intake"];
export type IntakeTrigger = Schemas["IntakeTrigger"];
export type ItemEvent = Schemas["Event"];
export type EventPage = Schemas["EventPage"];
export type Run = Schemas["Run"];
export type RunList = Schemas["RunList"];
export type RunStatus = Schemas["RunStatus"];
export type OrgRunList = Schemas["OrgRunList"];
export type OrgRun = Schemas["OrgRun"];
export type RunItem = Schemas["RunItem"];
export type RunSummary = Schemas["RunSummary"];
export type FactorySpend = Schemas["FactorySpend"];
export type NewNote = Schemas["NewNote"];
export type NoteReceipt = Schemas["NoteReceipt"];
export type AndonPull = Schemas["AndonPull"];
export type AndonState = Schemas["AndonState"];
export type GateTally = Schemas["GateTally"];
export type OrgAttention = Schemas["OrgAttention"];
export type OrgSearch = Schemas["OrgSearch"];
export type SearchItem = Schemas["SearchItem"];
export type AgentHit = Schemas["AgentHit"];
export type FactoryHit = Schemas["FactoryHit"];
export type ConnectionHit = Schemas["ConnectionHit"];
export type FactoryAttention = Schemas["FactoryAttention"];
export type FactoryOverviewList = Schemas["FactoryOverviewList"];
export type FactoryOverview = Schemas["FactoryOverview"];
export type FactoryHealth = Schemas["FactoryHealth"];
export type Inbox = Schemas["Inbox"];
export type InboxEntry = Schemas["InboxEntry"];
export type InboxEntryKind = Schemas["InboxEntryKind"];
export type ApprovalBrief = Schemas["ApprovalBrief"];
export type Touch = Schemas["Touch"];
export type TouchReason = Schemas["TouchReason"];
export type TouchReasonChange = Schemas["TouchReasonChange"];
export type ItemTouches = Schemas["ItemTouches"];
export type TouchHeadline = Schemas["TouchHeadline"];
export type TouchTrend = Schemas["TouchTrend"];
export type TouchCount = Schemas["TouchCount"];
export type TouchReasonCount = Schemas["TouchReasonCount"];
export type VersionTouches = Schemas["VersionTouches"];
export type TouchStats = Schemas["TouchStats"];
export type TouchWeek = Schemas["TouchWeek"];
export type TouchMetrics = Schemas["TouchMetrics"];
export type OrgTouchMetrics = Schemas["OrgTouchMetrics"];
export type FactoryTouchMetrics = Schemas["FactoryTouchMetrics"];

/** createGoalClient returns a typed fetch client for goald at baseUrl ("" for the same origin). */
export function createGoalClient(baseUrl: string, options: Omit<ClientOptions, "baseUrl"> = {}) {
  return createClient<paths>({ ...options, baseUrl });
}

export type GoalClient = ReturnType<typeof createGoalClient>;

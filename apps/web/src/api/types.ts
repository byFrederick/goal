// The contract's shapes, generated from contracts/api/openapi.yaml into @goal/api-client, under the names
// the screens use. Nothing here is written by hand: a shape changes in the contract first.
import type { components, ItemPage, MergeRequest } from '@goal/api-client';

export type {
  Activity,
  ActivityEvent,
  Actor,
  AgentHit,
  AgentStats,
  AndonPull,
  ApiKey,
  ApprovalBrief,
  AssemblyPart,
  AssemblyParts,
  AuditEntry,
  AuditPage,
  AuthConfig,
  AuthMode,
  BakeOff,
  BakeOffApply,
  BakeOffArm,
  BakeOffArmSummary,
  BakeOffCase,
  BakeOffCaseArm,
  BakeOffJudged,
  BakeOffList,
  BakeOffStarted,
  Board,
  BoardLane,
  BoardLine,
  BoardStation,
  BridgeStep,
  BuilderCapabilities,
  BuilderLimits,
  BuilderMeta,
  BuilderModel,
  Change,
  CheckResult,
  ClassOfService,
  CompareSide,
  Comparison,
  Connection,
  ConnectionEvent,
  ConnectionHit,
  ConnectionKind,
  ConnectionKindField,
  ConnectionKindFile,
  ConnectionPatch,
  ConnectionTestResult,
  ConstraintCandidate,
  ConstraintReport,
  ConstraintSource,
  CreatedApiKey,
  CreatedPersonalToken,
  Credential,
  CredentialPatch,
  CredentialType,
  CredentialUsers,
  CurrentRun,
  DefinitionCounts,
  DefinitionEvent,
  DefinitionProblem,
  DeviceRequest,
  Draft,
  DraftModel,
  DraftRef,
  DraftTree,
  EvalCaseEntry,
  EvalCaseFromRun,
  EvalCheckedBy,
  EvalEstimate,
  EvalHistoryTry,
  EvalList,
  EvalResultState,
  EvalRunRequest,
  EvalRunStarted,
  EvalSummary,
  EventPage,
  Factory,
  FactoryAttention,
  FactoryEvent,
  FactoryHealth,
  FactoryHit,
  FactoryList,
  FactoryModel,
  FactoryOverview,
  FactoryOverviewList,
  FactoryRole,
  FactoryRoleGrant,
  FactorySlots,
  FactorySpend,
  FactoryStart,
  FileChange,
  FileContent,
  FileInfo,
  FileMap,
  Inbox,
  InboxEntry,
  InboxEntryKind,
  Intake,
  IntakeEvent,
  IntakeTrigger,
  IntakeTriggerDetail,
  Invitation,
  Item,
  ItemAnswer,
  ItemAt,
  ItemCard,
  ItemEvent,
  ItemFlow,
  ItemQuestion,
  ItemSpan,
  ItemState,
  ItemsAtRemovedStations,
  ItemTouches,
  ItemWaiting,
  KnowledgeDocument,
  KnowledgeDocumentPage,
  KnowledgeDocumentText,
  KnowledgeEvent,
  KnowledgeHit,
  KnowledgeSource,
  KnowledgeSourceKind,
  KnowledgeSourcePatch,
  LeadTimeBridge,
  LineRange,
  Me,
  Member,
  Metrics,
  MetricsSummary,
  NewApiKey,
  NewConnection,
  NewFactoryRequest,
  NewItem,
  NewKnowledgeSource,
  NewOrg,
  NewPartRequest,
  NewPersonalToken,
  NoteReceipt,
  Org,
  OrgAttention,
  OrgDeleted,
  OrgRole,
  OrgRun,
  OrgRunList,
  OrgSearch,
  OrgSettings,
  OrgSettingsPatch,
  OrgSlots,
  OrgSummary,
  ParseError,
  PartOf,
  PartsSummary,
  PatchOp,
  Permission,
  PersonalToken,
  Problem,
  ProblemFix,
  PublishRequest,
  Ranges,
  RestoreRequest,
  Run,
  RunItem,
  RunList,
  RunStatus,
  RunSummary,
  SaveResult,
  SearchItem,
  Section,
  SectionETags,
  SectionSave,
  SectionSummary,
  SignInMethod,
  SkillFiles,
  SkillListing,
  SkillPage,
  Station,
  StationAction,
  StationKind,
  StationMoves,
  StreamEvent,
  Targets,
  Template,
  TemplateSummary,
  ToolApproval,
  ToolDecision,
  Touch,
  TouchCount,
  TouchHeadline,
  TouchMetrics,
  TouchReason,
  TouchReasonCount,
  TouchStats,
  TouchTrend,
  User,
  ValidateDraftRequest,
  Validation,
  Version,
  VersionDetail,
  VersionModel,
  VersionPage,
  VersionRef,
  VersionSource,
  VersionState,
  VersionTouches,
  WeeklyThroughput,
} from '@goal/api-client';

/** One page of a factory's items (`ItemPage` in the contract). */
export type ItemList = ItemPage;

/** How goald merges a pull request (POST /v1/orgs/{org}/items/{item}/merge). */
export type MergeMethod = NonNullable<MergeRequest['method']>;

// The spend shapes are the contract's, read from the generated schema (the api-client does not re-export them).
type Schemas = components['schemas'];
// The credential shapes a create or an import sends and answers (ADR 0017), read from the generated schema.
export type NewCredential = Schemas['NewCredential'];
export type CredentialImport = Schemas['CredentialImport'];
export type CredentialImportResult = Schemas['CredentialImportResult'];
export type CredentialImportOutcome = Schemas['CredentialImportOutcome'];
// What a code host connection reaches (GET …/connections/{connection}/repositories), for the Code step.
export type RepositoryList = Schemas['RepositoryList'];
export type RepositoryOwner = Schemas['RepositoryOwner'];
export type Repository = Schemas['Repository'];
// The organization's knowledge usage (ADR 0027): where a source's documents were used, and the runs of one document.
export type KnowledgeUsage = Schemas['KnowledgeUsage'];
export type KnowledgeDocumentUsage = Schemas['KnowledgeDocumentUsage'];
export type KnowledgeDocumentUsePage = Schemas['KnowledgeDocumentUsePage'];
export type KnowledgeDocumentUse = Schemas['KnowledgeDocumentUse'];
export type AgentSpend = Schemas['AgentSpend'];
export type ModelSpend = Schemas['ModelSpend'];
export type OrgSpend = Schemas['OrgSpend'];
/** A saved view of the Items page (ADR 0020): its filters are the page's URL search params, each key with its values. */
export type SavedView = Schemas['SavedView'];
export type SavedViewFilters = Schemas['SavedViewFilters'];
export type SavedViewList = Schemas['SavedViewList'];
export type NewSavedView = Schemas['NewSavedView'];
export type SavedViewChange = Schemas['SavedViewChange'];
/** The organization's policy (ADR 0024): what it allows every factory, the facts about it, and its parts. */
export type OrgPolicyInput = Schemas['OrgPolicyInput'];
export type OrgPolicyView = Schemas['OrgPolicyView'];
export type ToolPolicy = Schemas['ToolPolicy'];
export type PolicyModel = Schemas['PolicyModel'];
export type PolicyBoxSize = Schemas['PolicyBoxSize'];
export type SpendBudget = Schemas['SpendBudget'];
export type SpendBudgetState = Schemas['SpendBudgetState'];
export type SpendDay = Schemas['SpendDay'];
export type SpendWindow = Schemas['SpendWindow'];
export type WeekCost = Schemas['WeekCost'];

// The judges API's shapes (ADR 0025), read from the generated schema.
export type JudgeList = Schemas['JudgeList'];
export type JudgeListEntry = Schemas['JudgeListEntry'];
export type Judge = Schemas['Judge'];
export type JudgeWrite = Schemas['JudgeWrite'];
export type CreateJudgeRequest = Schemas['CreateJudgeRequest'];
export type JudgeOutcome = Schemas['JudgeOutcome'];
export type JudgeSummary = Schemas['JudgeSummary'];
export type JudgeDay = Schemas['JudgeDay'];
export type JudgeScore = Schemas['JudgeScore'];
export type JudgeRunsOn = Schemas['JudgeRunsOn'];
export type JudgeTries = Schemas['JudgeTries'];
export type TriggerTry = Schemas['TriggerTry'];
export type TriggerTryStep = Schemas['TriggerTryStep'];

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
  CurrentRun,
  DefinitionCounts,
  DefinitionEvent,
  DefinitionProblem,
  DeviceRequest,
  Draft,
  DraftModel,
  DraftRef,
  DraftRequest,
  DraftRequestList,
  DraftTree,
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
  KnowledgeCheck,
  KnowledgeCheckInput,
  KnowledgeEvent,
  KnowledgeFile,
  KnowledgeFilePage,
  KnowledgeReadCount,
  KnowledgeSource,
  KnowledgeSourceKind,
  KnowledgeSourcePatch,
  KnowledgeSourceReader,
  LeadTimeBridge,
  LineRange,
  Me,
  Member,
  Metrics,
  MetricsSummary,
  NewApiKey,
  NewConnection,
  NewDraftRequest,
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
  ReworkLoop,
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
  SignupDomain,
  SignupDomainInput,
  SkillFiles,
  SkillListing,
  SkillPage,
  Station,
  StationAction,
  StationKind,
  StationMoves,
  StreamEvent,
  Targets,
  Teardown,
  TeardownRun,
  TeardownStation,
  TeardownTouch,
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
// The secret shapes (ADR 0037), read from the generated schema. A value is in NewSecret, SecretValue and
// SecretImport only, which go out; no answer carries one.
export type Secret = Schemas['Secret'];
/** A standing rule (Always allow): one agent role, one tool; keyed by server for a factory on secrets (ADR 0037 decision 8). */
export type StandingRule = Schemas['StandingRule'];
// The Access read model (ADR 0037 decision 14): what every factory's agents reach.
export type Access = Schemas['Access'];
export type AccessGroup = Schemas['AccessGroup'];
export type AccessRow = Schemas['AccessRow'];
export type AccessUse = Schemas['AccessUse'];
export type AccessStatus = Schemas['AccessStatus'];
export type AccessView = Access['by'];
export type SecretUse = Schemas['SecretUse'];
export type SecretScope = Schemas['SecretScope'];
export type NewSecret = Schemas['NewSecret'];
export type RepositoryTestRequest = Schemas['RepositoryTestRequest'];
export type RepositoryTestResult = Schemas['RepositoryTestResult'];
export type RepositoryCheck = Schemas['RepositoryCheck'];
export type SecretPatch = Schemas['SecretPatch'];
export type SecretImport = Schemas['SecretImport'];
export type SecretImportResult = Schemas['SecretImportResult'];
// The tool catalog and the environment test (ADR 0037 decisions 14 and 15).
export type ToolTemplate = Schemas['ToolTemplate'];
export type TemplateSecret = Schemas['TemplateSecret'];
export type TemplateAdds = Schemas['TemplateAdds'];
export type TemplateMCP = Schemas['TemplateMCP'];
export type EnvironmentTestRequest = Schemas['EnvironmentTestRequest'];
export type EnvironmentTestResult = Schemas['EnvironmentTestResult'];
export type ServerTest = Schemas['ServerTest'];
export type SecretImportOutcome = Schemas['SecretImportOutcome'];
// The signed-in person's notification preferences (ADR 0023, decision 1): the events, their channels and the email.
export type NotificationPreferences = Schemas['NotificationPreferences'];
export type OrgNotificationSettings = Schemas['NotificationSettings'];
export type NotificationTestResult = Schemas['NotificationTestResult'];
export type NotificationEvents = Schemas['NotificationEvents'];
export type NotificationChannels = Schemas['NotificationChannels'];
// What a code host connection reaches (GET …/connections/{connection}/repositories), for the Code step.
export type RepositoryList = Schemas['RepositoryList'];
export type RepositoryOwner = Schemas['RepositoryOwner'];
export type Repository = Schemas['Repository'];
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

export type JonahOverview = Schemas['JonahOverview'];
export type JonahPolicy = Schemas['JonahPolicy'];
export type JonahObservation = Schemas['JonahObservation'];
export type JonahReview = Schemas['JonahReview'];
export type ProposalState = Schemas['ProposalState'];
export type ProposalEntry = Schemas['ProposalEntry'];
export type ProposalEstimate = Schemas['ProposalEstimate'];
export type ProposalPage = Schemas['ProposalPage'];
export type Proposal = Schemas['Proposal'];
export type ProposalFile = Schemas['ProposalFile'];
export type ProposalEvidence = Schemas['ProposalEvidence'];
export type ProposalDecided = Schemas['ProposalDecided'];
export type ProposalTried = Schemas['ProposalTried'];
export type TriedItem = Schemas['TriedItem'];
export type TriedTry = Schemas['TriedTry'];
export type KeptTest = Schemas['KeptTest'];
export type KeptTestList = Schemas['KeptTestList'];
export type Watch = Schemas['Watch'];
export type WatchVerdict = Schemas['WatchVerdict'];
export type TriggerTry = Schemas['TriggerTry'];
export type TriggerTryStep = Schemas['TriggerTryStep'];

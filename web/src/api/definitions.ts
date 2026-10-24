// The definitions routes (B §8) over client.ts's goald client: factories, the one shared draft per
// factory (Visual sections, single files, the whole tree), validation, publishing, versions,
// compare, restore and templates. Every function takes the organization (slug or id) and the
// factory (id or name) first, and throws ApiError, whose `code` names the refusal: 412
// `draft-conflict` carries the current value (`problem.value` and `problem.etag` for a section,
// `problem.sha256` for a file), 409 `definition-invalid` the problems (`problem.problems`), 409
// `yaml-invalid` where factory.yaml stops parsing (`problem.parse_error`).
//
// Writes are conditional: a section save sends the section's etag, a file write the sha256 it
// read (or `create` for a new file), a discard the draft's revision; goald refuses a write without
// one (428 `precondition-required`). Every successful draft write is remembered by the draft's id
// and the revision it made (savedHere), so the stream's echo of this tab's own save refetches
// nothing.
import { orgPath, unwrap } from '@goal/api-client';
import { cancel, goal } from './client';
import type {
  Comparison,
  Draft,
  DraftModel,
  DraftTree,
  Factory,
  FileInfo,
  FileMap,
  NewFactoryRequest,
  PublishRequest,
  RestoreRequest,
  SaveResult,
  Section,
  SectionSave,
  Template,
  TemplateSummary,
  ValidateDraftRequest,
  Validation,
  Version,
  VersionDetail,
  VersionModel,
  VersionPage,
} from './types';

/**
 * The drafts this tab knows and saved, by factory: the open draft's id (from the last draft read
 * or opened) and, by draft id, the revisions this tab's writes made. A new draft's revisions start
 * again at 1, so a revision alone does not name a save.
 */
const openDrafts = new Map<string, string>();
const savedRevisions = new Map<string, Set<number>>();

const draftKey = (factory: string, draft: string) => `${factory}/${draft}`;

/** Remembers the factory's open draft as goald answered it. */
function known(factory: string, draft: Draft): Draft {
  openDrafts.set(factory, draft.id);
  return draft;
}

/** Remembers a revision this tab's write made of a draft. */
function remember(factory: string, draft: string | undefined, revision: number): void {
  if (draft === undefined) return;
  const key = draftKey(factory, draft);
  const revisions = savedRevisions.get(key) ?? new Set<number>();
  revisions.add(revision);
  savedRevisions.set(key, revisions);
}

/** A write's result, remembered as a save of the factory's open draft. */
function saved(factory: string): (r: SaveResult) => SaveResult {
  return (r) => {
    remember(factory, openDrafts.get(factory), r.revision);
    return r;
  };
}

/** Whether this tab's write made revision of the factory's draft: the stream's event is its echo. */
export function savedHere(factory: string, draft: string | undefined, revision: number): boolean {
  return draft !== undefined && (savedRevisions.get(draftKey(factory, draft))?.has(revision) ?? false);
}

/** An entity tag: the If-Match of a conditional write. */
const tag = (value: string | number) => `"${value}"`;

/** Request bodies sent as they are (a file's text), not as JSON. */
const raw = { bodySerializer: (body: unknown) => body as string };

// Factories and templates (§8.1).

export function getFactory(org: string, factory: string, signal?: AbortSignal): Promise<Factory> {
  return unwrap(goal.GET(orgPath('/factories/{factory}'), { params: { path: { org, factory } }, ...cancel(signal) }));
}

/** Creates a factory with its first draft: blank, from a template, a clone or imported files. */
export function createFactory(org: string, request: NewFactoryRequest): Promise<Factory> {
  return unwrap(goal.POST(orgPath('/factories'), { params: { path: { org } }, body: request }));
}

/** Deletes a factory never published (the wizard's Cancel); 409 `factory-published` otherwise. */
export async function deleteFactory(org: string, factory: string): Promise<void> {
  await unwrap(goal.DELETE(orgPath('/factories/{factory}'), { params: { path: { org, factory } } }));
}

export function pauseFactory(org: string, factory: string): Promise<Factory> {
  return unwrap(goal.POST(orgPath('/factories/{factory}/pause'), { params: { path: { org, factory } } }));
}

export function resumeFactory(org: string, factory: string): Promise<Factory> {
  return unwrap(goal.POST(orgPath('/factories/{factory}/resume'), { params: { path: { org, factory } } }));
}

/** The latest version of every template of the catalog, by name. */
export async function listTemplates(org: string, signal?: AbortSignal): Promise<TemplateSummary[]> {
  const body = await unwrap(goal.GET(orgPath('/templates'), { params: { path: { org } }, ...cancel(signal) }));
  return body.templates;
}

/** One version of a template (the latest when `version` is absent), with a preview of its model. */
export function getTemplate(org: string, name: string, version?: number, signal?: AbortSignal): Promise<Template> {
  return unwrap(
    goal.GET(orgPath('/templates/{name}'), {
      params: { path: { org, name }, query: version !== undefined ? { version } : {} },
      ...cancel(signal),
    }),
  );
}

/** Validates files that belong to no factory yet (the Import YAML preview). */
export function validateDefinition(org: string, files: FileMap): Promise<Validation> {
  return unwrap(goal.POST(orgPath('/definitions/validate'), { params: { path: { org } }, body: { files } }));
}

/** Formats a factory.yaml the way Visual saves write it ("Format"); 422 `yaml-invalid` when it does not parse. */
export function formatDefinition(org: string, yaml: string): Promise<string> {
  return unwrap(
    goal.POST(orgPath('/definitions/format'), {
      params: { path: { org } },
      body: yaml,
      headers: { 'Content-Type': 'text/yaml' },
      parseAs: 'text',
      ...raw,
    }),
  ) as Promise<string>;
}

// The draft (§8.2).

/** The open draft with its changes and problems; 404 `no-draft` when none is open. */
export async function getDraft(org: string, factory: string, signal?: AbortSignal): Promise<Draft> {
  return known(
    factory,
    await unwrap(
      goal.GET(orgPath('/factories/{factory}/draft'), { params: { path: { org, factory } }, ...cancel(signal) }),
    ),
  );
}

export interface OpenDraftOptions {
  /** The published version to start from (a number or `current`); the current one when absent. */
  base?: string | undefined;
  /** Archive the open draft first. */
  replace?: boolean | undefined;
}

/** Opens the factory's draft, or returns the open one; 409 `draft-exists` when it is on another base. */
export async function openDraft(
  org: string,
  factory: string,
  { base, replace }: OpenDraftOptions = {},
): Promise<Draft> {
  const draft = await unwrap(
    goal.POST(orgPath('/factories/{factory}/draft'), {
      params: { path: { org, factory } },
      body: { ...(base ? { base } : {}), ...(replace ? { replace } : {}) },
    }),
  );
  remember(factory, known(factory, draft).id, draft.revision);
  return draft;
}

/** Discards the open draft at `revision`; 412 `draft-conflict` when it moved since. */
export async function discardDraft(org: string, factory: string, revision: number): Promise<void> {
  await unwrap(
    goal.DELETE(orgPath('/factories/{factory}/draft'), {
      params: { path: { org, factory }, header: { 'If-Match': tag(revision) } },
    }),
  );
  remember(factory, openDrafts.get(factory), revision);
  openDrafts.delete(factory);
}

/** The draft's model, section etags, edited fields and YAML ranges; 409 `yaml-invalid` while it does not parse. */
export function getDraftModel(org: string, factory: string, signal?: AbortSignal): Promise<DraftModel> {
  return unwrap(
    goal.GET(orgPath('/factories/{factory}/draft/model'), { params: { path: { org, factory } }, ...cancel(signal) }),
  );
}

/** Saves one section on the etag it was read with; 412 `draft-conflict` with the section as it is now. */
export function saveDraftSection(
  org: string,
  factory: string,
  section: Section,
  etag: string,
  save: SectionSave,
): Promise<SaveResult> {
  return unwrap(
    goal.PUT(orgPath('/factories/{factory}/draft/model/{section}'), {
      params: { path: { org, factory, section }, header: { 'If-Match': tag(etag) } },
      body: save,
    }),
  ).then(saved(factory));
}

/** Puts back the base's value at edited fields of a section ("Edited · Undo"). */
export function revertDraftSection(
  org: string,
  factory: string,
  section: Section,
  etag: string,
  pointers: string[],
): Promise<SaveResult> {
  return unwrap(
    goal.POST(orgPath('/factories/{factory}/draft/model/{section}/revert'), {
      params: { path: { org, factory, section }, header: { 'If-Match': tag(etag) } },
      body: { pointers },
    }),
  ).then(saved(factory));
}

export async function listDraftFiles(org: string, factory: string, signal?: AbortSignal): Promise<FileInfo[]> {
  const body = await unwrap(
    goal.GET(orgPath('/factories/{factory}/draft/files'), { params: { path: { org, factory } }, ...cancel(signal) }),
  );
  return body.files;
}

/** One file's text and the sha256 its writes are conditional on. */
export interface FileText {
  text: string;
  sha256: string;
}

/** The sha256 an ETag header names. */
const sha256Of = (response: Response) => (response.headers.get('ETag') ?? '').replaceAll('"', '');

/** One file of the draft, as stored; 404 `file-not-found`. */
export async function getDraftFile(
  org: string,
  factory: string,
  path: string,
  signal?: AbortSignal,
): Promise<FileText> {
  const call = goal.GET(orgPath('/factories/{factory}/draft/files/{path}'), {
    params: { path: { org, factory, path } },
    parseAs: 'text',
    ...cancel(signal),
  });
  const text = (await unwrap(call)) as string;
  return { text, sha256: sha256Of((await call).response) };
}

/** What a file write is based on: the sha256 the editor read, or nothing (`create`, the file must not exist). */
export type FileBase = { sha256: string } | { create: true };

/** Writes one file of the draft exactly as typed; 412 `draft-conflict` with its sha256 now. */
export function putDraftFile(
  org: string,
  factory: string,
  path: string,
  text: string,
  base: FileBase,
): Promise<SaveResult> {
  return unwrap(
    goal.PUT(orgPath('/factories/{factory}/draft/files/{path}'), {
      params: {
        path: { org, factory, path },
        header: 'create' in base ? { 'If-None-Match': '*' } : { 'If-Match': tag(base.sha256) },
      },
      body: text,
      headers: { 'Content-Type': 'application/octet-stream' },
      ...raw,
    }),
  ).then(saved(factory));
}

export function deleteDraftFile(org: string, factory: string, path: string, sha256: string): Promise<SaveResult> {
  return unwrap(
    goal.DELETE(orgPath('/factories/{factory}/draft/files/{path}'), {
      params: { path: { org, factory, path }, header: { 'If-Match': tag(sha256) } },
    }),
  ).then(saved(factory));
}

/** Makes `tree.files` the draft's whole tree; 412 when `expected_revision` is stale. */
export function replaceDraftTree(org: string, factory: string, tree: DraftTree): Promise<SaveResult> {
  return unwrap(
    goal.PUT(orgPath('/factories/{factory}/draft/tree'), { params: { path: { org, factory } }, body: tree }),
  ).then(saved(factory));
}

/** Validates the draft at its revision, with unsaved YAML (`overlay`) and the moves the publish will carry. */
export function validateDraft(
  org: string,
  factory: string,
  request: ValidateDraftRequest = {},
  signal?: AbortSignal,
): Promise<Validation> {
  return unwrap(
    goal.POST(orgPath('/factories/{factory}/draft/validate'), {
      params: { path: { org, factory } },
      body: request,
      ...cancel(signal),
    }),
  );
}

/** Publishes the open draft as the next version. */
export function publishDraft(org: string, factory: string, request: PublishRequest): Promise<Version> {
  return unwrap(
    goal.POST(orgPath('/factories/{factory}/draft/publish'), { params: { path: { org, factory } }, body: request }),
  );
}

// Versions and compare (§8.3).

export interface VersionPageQuery {
  /** The previous page's next_before. */
  before?: number | undefined;
  /** 1 to 50; goald's default is 20. */
  limit?: number | undefined;
}

export function listVersions(
  org: string,
  factory: string,
  { before, limit }: VersionPageQuery = {},
  signal?: AbortSignal,
): Promise<VersionPage> {
  return unwrap(
    goal.GET(orgPath('/factories/{factory}/versions'), {
      params: {
        path: { org, factory },
        query: { ...(before !== undefined ? { before } : {}), ...(limit !== undefined ? { limit } : {}) },
      },
      ...cancel(signal),
    }),
  );
}

/** `version` is a number, `current`, `draft`, `base` or `source`. */
export function getVersion(
  org: string,
  factory: string,
  version: string,
  signal?: AbortSignal,
): Promise<VersionDetail> {
  return unwrap(
    goal.GET(orgPath('/factories/{factory}/versions/{version}'), {
      params: { path: { org, factory, version } },
      ...cancel(signal),
    }),
  );
}

export function getVersionModel(
  org: string,
  factory: string,
  version: string,
  signal?: AbortSignal,
): Promise<VersionModel> {
  return unwrap(
    goal.GET(orgPath('/factories/{factory}/versions/{version}/model'), {
      params: { path: { org, factory, version } },
      ...cancel(signal),
    }),
  );
}

/** One file of a version: a published version's never changes, so it is cached by its sha256. */
export async function getVersionFile(
  org: string,
  factory: string,
  version: string,
  path: string,
  signal?: AbortSignal,
): Promise<FileText> {
  const call = goal.GET(orgPath('/factories/{factory}/versions/{version}/files/{path}'), {
    params: { path: { org, factory, version, path } },
    parseAs: 'text',
    ...cancel(signal),
  });
  const text = (await unwrap(call)) as string;
  return { text, sha256: sha256Of((await call).response) };
}

/** The semantic difference between two versions; the draft against its base by default. */
export function compareVersions(
  org: string,
  factory: string,
  from = 'base',
  to = 'draft',
  signal?: AbortSignal,
): Promise<Comparison> {
  return unwrap(
    goal.GET(orgPath('/factories/{factory}/compare'), {
      params: { path: { org, factory }, query: { from, to } },
      ...cancel(signal),
    }),
  );
}

/** Runs an older published version again; 409 `draft-base-moved` when the current one is not expected. */
export function restoreVersion(
  org: string,
  factory: string,
  version: string,
  request: RestoreRequest,
): Promise<Factory> {
  return unwrap(
    goal.POST(orgPath('/factories/{factory}/versions/{version}/restore'), {
      params: { path: { org, factory, version } },
      body: request,
    }),
  );
}

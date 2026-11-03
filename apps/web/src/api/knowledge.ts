// The organization's knowledge base (spec 4 §8.2) over client.ts's goald client: its sources,
// their documents, uploads (multipart, parts named files) and the search agents run through
// knowledge__search. An uploaded original is downloaded by the browser itself from
// documentContentUrl: goald answers it as an attachment.
import { orgPath, unwrap } from '@goal/api-client';
import { apiBase, cancel, goal } from './client';
import type {
  KnowledgeCheck,
  KnowledgeCheckInput,
  KnowledgeDocument,
  KnowledgeDocumentPage,
  KnowledgeDocumentText,
  KnowledgeDocumentUsePage,
  KnowledgeFile,
  KnowledgeFilePage,
  KnowledgeHit,
  KnowledgeReadCount,
  KnowledgeSource,
  KnowledgeSourcePatch,
  KnowledgeUsage,
  NewKnowledgeSource,
} from './types';

export interface KnowledgeSourceFilter {
  /** Only the sources this factory's current version may search (its id or name). */
  factory?: string;
}

/** The organization's sources, by name. */
export async function listKnowledgeSources(
  org: string,
  filter: KnowledgeSourceFilter = {},
  signal?: AbortSignal,
): Promise<KnowledgeSource[]> {
  const body = await unwrap(
    goal.GET(orgPath('/knowledge/sources'), {
      params: { path: { org }, query: filter.factory ? { factory: filter.factory } : {} },
      ...cancel(signal),
    }),
  );
  return body.sources;
}

/** One source; 404 knowledge-source-not-found. */
export function getKnowledgeSource(org: string, source: string, signal?: AbortSignal): Promise<KnowledgeSource> {
  return unwrap(
    goal.GET(orgPath('/knowledge/sources/{source}'), { params: { path: { org, source } }, ...cancel(signal) }),
  );
}

/** 400 for a broken rule or connection-cannot-sync; 404 connection-not-found; 409 knowledge-source-name-taken. */
export function createKnowledgeSource(org: string, source: NewKnowledgeSource): Promise<KnowledgeSource> {
  return unwrap(goal.POST(orgPath('/knowledge/sources'), { params: { path: { org } }, body: source }));
}

/** Its title, config (replaced whole) or schedule. */
export function updateKnowledgeSource(
  org: string,
  source: string,
  patch: KnowledgeSourcePatch,
): Promise<KnowledgeSource> {
  return unwrap(goal.PATCH(orgPath('/knowledge/sources/{source}'), { params: { path: { org, source } }, body: patch }));
}

/** With its documents; 409 knowledge-source-in-use while a factory's current version lists it. */
export async function deleteKnowledgeSource(org: string, source: string): Promise<void> {
  await unwrap(goal.DELETE(orgPath('/knowledge/sources/{source}'), { params: { path: { org, source } } }));
}

/** Makes it due now; 409 knowledge-source-live for a live source. */
export async function syncKnowledgeSource(org: string, source: string): Promise<void> {
  await unwrap(goal.POST(orgPath('/knowledge/sources/{source}/sync'), { params: { path: { org, source } } }));
}

/** One page of a source's documents in id order, after the id `after`. */
export function listKnowledgeDocuments(
  org: string,
  source: string,
  page: { after?: string | undefined; limit: number },
  signal?: AbortSignal,
): Promise<KnowledgeDocumentPage> {
  return unwrap(
    goal.GET(orgPath('/knowledge/sources/{source}/documents'), {
      params: { path: { org, source }, query: { limit: page.limit, ...(page.after ? { after: page.after } : {}) } },
      ...cancel(signal),
    }),
  );
}

/**
 * Files for an upload source, pending until indexed. goald checks every file of a request before
 * it stores any: 413 upload-too-large, 415 unsupported-document-type, 409 not-an-upload-source.
 */
export async function uploadKnowledgeDocuments(
  org: string,
  source: string,
  files: readonly File[],
): Promise<KnowledgeDocument[]> {
  const form = new FormData();
  for (const f of files) form.append('files', f, f.name);
  const body = await unwrap(
    goal.POST(orgPath('/knowledge/sources/{source}/documents'), {
      params: { path: { org, source } },
      // The contract types the parts as strings; the form carries the files themselves.
      body: { files: [] },
      bodySerializer: () => form,
    }),
  );
  return body.documents;
}

/** A document with its indexed text (empty until indexed); 404 knowledge-document-not-found. */
export function getKnowledgeDocument(
  org: string,
  document: string,
  signal?: AbortSignal,
): Promise<KnowledgeDocumentText> {
  return unwrap(
    goal.GET(orgPath('/knowledge/documents/{document}'), { params: { path: { org, document } }, ...cancel(signal) }),
  );
}

/** An uploaded document; 409 not-an-upload-source for a synced one. */
export async function deleteKnowledgeDocument(org: string, document: string): Promise<void> {
  await unwrap(goal.DELETE(orgPath('/knowledge/documents/{document}'), { params: { path: { org, document } } }));
}

/** Where the browser downloads an uploaded original (an attachment, never rendered). */
export function documentContentUrl(org: string, document: string): string {
  return `${apiBase}/v1/orgs/${encodeURIComponent(org)}/knowledge/documents/${encodeURIComponent(document)}/content`;
}

export interface KnowledgeQuery {
  q: string;
  /** Only these sources; every source when absent. */
  sources?: readonly string[] | undefined;
  limit?: number;
}

/** What knowledge__search returns for the query, best first. */
export async function searchKnowledge(
  org: string,
  { q, sources, limit }: KnowledgeQuery,
  signal?: AbortSignal,
): Promise<KnowledgeHit[]> {
  const body = await unwrap(
    goal.GET(orgPath('/knowledge/search'), {
      params: {
        path: { org },
        query: { q, ...(sources ? { source: [...sources] } : {}), ...(limit ? { limit } : {}) },
      },
      ...cancel(signal),
    }),
  );
  return body.hits;
}

/** Where a source's documents were used over the last `days` days (7 when absent); 404 knowledge-source-not-found. */
export function getKnowledgeUsage(
  org: string,
  source: string,
  days?: number,
  signal?: AbortSignal,
): Promise<KnowledgeUsage> {
  return unwrap(
    goal.GET(orgPath('/knowledge/usage'), {
      params: { path: { org }, query: { source, ...(days ? { days } : {}) } },
      ...cancel(signal),
    }),
  );
}

/** One page of the runs that used a document, newest first; 404 knowledge-document-not-found. */
export function listKnowledgeDocumentUses(
  org: string,
  document: string,
  page: { after?: string | undefined; limit: number },
  signal?: AbortSignal,
): Promise<KnowledgeDocumentUsePage> {
  return unwrap(
    goal.GET(orgPath('/knowledge/documents/{document}/uses'), {
      params: { path: { org, document }, query: { limit: page.limit, ...(page.after ? { after: page.after } : {}) } },
      ...cancel(signal),
    }),
  );
}

/** Checks a source's settings as Add a source has them, storing nothing. */
export function checkKnowledgeSource(
  org: string,
  input: KnowledgeCheckInput,
  signal?: AbortSignal,
): Promise<KnowledgeCheck> {
  return unwrap(goal.POST(orgPath('/knowledge/check'), { params: { path: { org } }, body: input, ...cancel(signal) }));
}

/** The files whose path or title starts with `find`, across every source. */
export async function findKnowledgeFiles(
  org: string,
  find: string,
  limit?: number,
  signal?: AbortSignal,
): Promise<KnowledgeFile[]> {
  const body = await unwrap(
    goal.GET(orgPath('/knowledge/files'), {
      params: { path: { org }, query: { find, ...(limit ? { limit } : {}) } },
      ...cancel(signal),
    }),
  );
  return body.files;
}

/** One page of a source's current files in path order; `find` narrows them by path or title. */
export function listKnowledgeFiles(
  org: string,
  source: string,
  page: { find?: string | undefined; after?: string | undefined; limit?: number },
  signal?: AbortSignal,
): Promise<KnowledgeFilePage> {
  return unwrap(
    goal.GET(orgPath('/knowledge/sources/{source}/files'), {
      params: {
        path: { org, source },
        query: {
          ...(page.find ? { find: page.find } : {}),
          ...(page.after ? { after: page.after } : {}),
          ...(page.limit ? { limit: page.limit } : {}),
        },
      },
      ...cancel(signal),
    }),
  );
}

/** The daily reads and searches of one source, or of every one, over the last `days` days (7 when absent). */
export async function listKnowledgeReads(
  org: string,
  filter: { source?: string | undefined; days?: number } = {},
  signal?: AbortSignal,
): Promise<KnowledgeReadCount[]> {
  const body = await unwrap(
    goal.GET(orgPath('/knowledge/reads'), {
      params: {
        path: { org },
        query: { ...(filter.source ? { source: filter.source } : {}), ...(filter.days ? { days: filter.days } : {}) },
      },
      ...cancel(signal),
    }),
  );
  return body.reads;
}

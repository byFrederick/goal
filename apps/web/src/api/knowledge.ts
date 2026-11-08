// The organization's knowledge as files (ADR 0038) over client.ts's goald client: its sources,
// the files of their current snapshots, uploads (multipart, parts named files) and how often
// agents read them.
import { orgPath, unwrap } from '@goal/api-client';
import { cancel, goal } from './client';
import type {
  KnowledgeCheck,
  KnowledgeCheckInput,
  KnowledgeFile,
  KnowledgeFilePage,
  KnowledgeReadCount,
  KnowledgeSource,
  KnowledgeSourcePatch,
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

/** 400 for a broken rule; 404 secret-not-found; 409 knowledge-source-name-taken. */
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

/** With its snapshots and uploads; 409 knowledge-source-in-use while a factory's current version lists it. */
export async function deleteKnowledgeSource(org: string, source: string): Promise<void> {
  await unwrap(goal.DELETE(orgPath('/knowledge/sources/{source}'), { params: { path: { org, source } } }));
}

/** Makes it due now. */
export async function syncKnowledgeSource(org: string, source: string): Promise<void> {
  await unwrap(goal.POST(orgPath('/knowledge/sources/{source}/sync'), { params: { path: { org, source } } }));
}

/**
 * Files for an upload source, each at the path its file name gives. goald checks every file of a
 * request before it stores any: 413 upload-too-large, 415 unsupported-document-type, 409
 * not-an-upload-source. The next sync writes the snapshot that holds them.
 */
export async function uploadKnowledgeFiles(org: string, source: string, files: readonly File[]): Promise<void> {
  const form = new FormData();
  for (const f of files) form.append('files', f, f.name);
  await unwrap(
    goal.POST(orgPath('/knowledge/sources/{source}/uploads'), {
      params: { path: { org, source } },
      // The contract types the parts as strings; the form carries the files themselves.
      body: { files: [] },
      bodySerializer: () => form,
    }),
  );
}

/** Deletes the upload at `path`; the next sync leaves the file out of the snapshot. 404 knowledge-file-not-found. */
export async function deleteKnowledgeUpload(org: string, source: string, path: string): Promise<void> {
  await unwrap(
    goal.DELETE(orgPath('/knowledge/sources/{source}/uploads'), { params: { path: { org, source }, query: { path } } }),
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

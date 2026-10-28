// The CSV exports of the Runs and Spend screens (ADR 0020, decision 3). goald streams the file and names
// it in Content-Disposition; the page asks for it through the shared client as a Blob, so a problem
// (413 export-too-large, 400) comes back as an ApiError the screen can show. The CSV is never built here.
import { ApiError, problemOf, type Result } from '@goal/api-client';

/** A CSV export: the file as goald streamed it, and the name goald gave it. */
export interface CsvFile {
  blob: Blob;
  filename: string;
}

/** The filename in a Content-Disposition header (attachment; filename="runs-2026-10-27.csv"), or the fallback. */
export function filenameOf(disposition: string | null, fallback: string): string {
  const match = disposition ? /filename="([^"]+)"/i.exec(disposition) : null;
  return match?.[1] ?? fallback;
}

/** The file of a successful export call, or the ApiError of a failed one (its problem, as every call throws). */
export function csvFileOf(result: Result<Blob>, fallback: string): CsvFile {
  if (!result.response.ok) throw new ApiError(problemOf(result.response, result.error));
  return {
    blob: result.data as Blob,
    filename: filenameOf(result.response.headers.get('Content-Disposition'), fallback),
  };
}

/** Saves the file through a temporary link, so the browser downloads it under goald's filename. */
export function saveCsv({ blob, filename }: CsvFile): void {
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.append(link);
  link.click();
  link.remove();
  // Some browsers still need the object URL while the download starts; it is released a little later.
  window.setTimeout(() => URL.revokeObjectURL(url), 10_000);
}

/** What the screen says when an export fails: the problem's title and detail, or a plain sentence for no answer. */
export function exportFailure(error: unknown, what: string): string {
  return error instanceof ApiError ? error.message : `Couldn’t export ${what}. Check your connection and try again.`;
}

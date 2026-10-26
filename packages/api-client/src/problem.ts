// Errors as RFC 9457 problem details. goald answers every failure with a Problem
// (contracts/api/openapi.yaml, responses.Problem); a proxy or a crash may answer with something else, so
// the problem is rebuilt from the status line when the body is not one.
import type { components } from "./schema";

export type Problem = components["schemas"]["Problem"];

/** ApiError is thrown for every response that is not a success; it carries the Problem. */
export class ApiError extends Error {
  readonly problem: Problem;

  constructor(problem: Problem) {
    super(problem.detail ? `${problem.title}: ${problem.detail}` : problem.title);
    this.name = "ApiError";
    this.problem = problem;
  }

  get status(): number {
    return this.problem.status;
  }

  get title(): string {
    return this.problem.title;
  }

  get detail(): string | undefined {
    return this.problem.detail;
  }

  /** The stable name of the error (slug-taken, draft-conflict, …), when goald gives one. */
  get code(): string | undefined {
    return this.problem.code;
  }
}

function isProblem(body: unknown): body is Pick<Problem, "title"> & Partial<Problem> {
  return typeof body === "object" && body !== null && typeof (body as { title?: unknown }).title === "string";
}

/**
 * problemOf reads the Problem of a failed response from its parsed body, with its code and the
 * extension members a client acts on (a refused publish's problems, the current value under a
 * stale write); the status always comes from the response.
 */
export function problemOf(response: Response, body: unknown): Problem {
  if (isProblem(body)) {
    const { detail, ...rest } = body;
    return {
      ...rest,
      title: body.title,
      status: response.status,
      ...(typeof detail === "string" && detail !== "" ? { detail } : {}),
    };
  }
  return { title: response.statusText || "Request failed", status: response.status };
}

/** What an openapi-fetch call resolves to: data on success, the parsed error body otherwise. */
export type Result<T> = { data: T; error?: never; response: Response } | { data?: never; error: unknown; response: Response };

/**
 * unwrap resolves to the data of a successful call and throws ApiError otherwise.
 *
 *   const board = await unwrap(goal.GET("/v1/factories/{factory}/board", { params }));
 */
export async function unwrap<T>(call: Promise<Result<T>>): Promise<T> {
  const result = await call;
  if (!result.response.ok) throw new ApiError(problemOf(result.response, result.error));
  return result.data as T;
}

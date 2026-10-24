import { describe, expect, test } from "vitest";
import { createGoalClient } from "./index";
import { ApiError, unwrap } from "./problem";
import { orgPath } from "./routes";

// goald behind a fake fetch: each test decides the one response.
function goaldAnswering(response: Response) {
  const requests: Request[] = [];
  const goal = createGoalClient("http://goald.example.com", {
    fetch: async (request: Request) => {
      requests.push(request);
      return response.clone();
    },
  });
  return { goal, requests };
}

const problemJson = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/problem+json" } });

describe("unwrap", () => {
  test("resolves to the data of a success", async () => {
    const { goal, requests } = goaldAnswering(Response.json({ factories: [] }));
    await expect(unwrap(goal.GET(orgPath("/factories"), { params: { path: { org: "acme" } } }))).resolves.toEqual({ factories: [] });
    expect(requests[0]?.url).toBe("http://goald.example.com/v1/orgs/acme/factories");
  });

  test("throws the Problem goald answered with", async () => {
    const { goal } = goaldAnswering(problemJson(409, { title: "Item is not blocked", status: 409, detail: "goal#2 is done" }));
    const call = unwrap(goal.POST(orgPath("/items/{item}/retry"), { params: { path: { org: "acme", item: "goal#2" } } }));

    const err = await call.catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).problem).toEqual({ title: "Item is not blocked", status: 409, detail: "goal#2 is done" });
    expect((err as ApiError).message).toBe("Item is not blocked: goal#2 is done");
  });

  test("keeps the Problem's code for clients to branch on", async () => {
    const { goal } = goaldAnswering(problemJson(409, { title: "Conflict", status: 409, detail: 'slug "example-labs" is taken', code: "slug-taken" }));
    const err = await unwrap(goal.PATCH(orgPath("/settings"), { params: { path: { org: "acme" } }, body: { slug: "example-labs" } })).catch((e: unknown) => e);
    expect((err as ApiError).problem).toEqual({ title: "Conflict", status: 409, detail: 'slug "example-labs" is taken', code: "slug-taken" });
    expect((err as ApiError).code).toBe("slug-taken");
  });

  test("keeps the code and what a client acts on: the current value under a stale write", async () => {
    const body = { title: "Precondition Failed", status: 412, code: "draft-conflict", section: "line", etag: "e2", value: { stations: [] } };
    const { goal } = goaldAnswering(problemJson(412, body));
    const err = await unwrap(
      goal.PUT(orgPath("/factories/{factory}/draft/model/{section}"), {
        params: { path: { org: "acme", factory: "goal", section: "line" }, header: { "If-Match": '"e1"' } },
        body: { value: {} },
      }),
    ).catch((e: unknown) => e);

    expect((err as ApiError).code).toBe("draft-conflict");
    expect((err as ApiError).problem).toEqual(body);
  });

  test("percent-encodes a file path, slashes included, as one path segment", async () => {
    const { goal, requests } = goaldAnswering(new Response("# engineer\n"));
    await goal.GET(orgPath("/factories/{factory}/draft/files/{path}"), {
      params: { path: { org: "acme", factory: "goal", path: "agents/engineer.md" } },
      parseAs: "text",
    });
    expect(requests[0]?.url).toBe("http://goald.example.com/v1/orgs/acme/factories/goal/draft/files/agents%2Fengineer.md");
  });

  test("percent-encodes an item reference in the path", async () => {
    const { goal, requests } = goaldAnswering(Response.json({}));
    await unwrap(goal.GET(orgPath("/items/{item}"), { params: { path: { org: "acme", item: "goal#2" } } }));
    expect(requests[0]?.url).toBe("http://goald.example.com/v1/orgs/acme/items/goal%232");
  });

  test("takes the status from the response, not the body", async () => {
    const { goal } = goaldAnswering(problemJson(404, { title: "Factory not found", status: 500 }));
    const err = await unwrap(goal.GET(orgPath("/factories"), { params: { path: { org: "acme" } } })).catch((e: unknown) => e);
    expect((err as ApiError).status).toBe(404);
  });

  test("builds a Problem from the status line when the body is not one", async () => {
    const { goal } = goaldAnswering(new Response("<html>Bad gateway</html>", { status: 502, statusText: "Bad Gateway" }));
    const err = await unwrap(goal.GET(orgPath("/factories"), { params: { path: { org: "acme" } } })).catch((e: unknown) => e);
    expect((err as ApiError).problem).toEqual({ title: "Bad Gateway", status: 502 });
  });
});

import { describe, expect, test } from "vitest";
import { routeUrl } from "./routes";

describe("routeUrl", () => {
  test("fills path placeholders, percent-encoded, and puts the rest in the query", () => {
    expect(routeUrl("http://goald.example.com/", "/v1/orgs/{org}/stream", { org: "acme labs", factory: "sre" })).toBe(
      "http://goald.example.com/v1/orgs/acme%20labs/stream?factory=sre",
    );
  });

  test("leaves out undefined and empty params", () => {
    expect(routeUrl("", "/v1/stream", { factory: undefined, station: "" })).toBe("/v1/stream");
  });
});

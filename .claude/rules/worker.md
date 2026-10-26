---
paths:
  - "apps/worker/**"
---

# Worker

The worker is the one agent image every run uses. It is a box contract: the platform calls `goal-agent prepare` and then `goal-agent run`, and nothing else. The decisions behind it: [ADR 0008](../../docs/adr/0008-portable-runs-connections-and-knowledge.md). The run's flow: [spec 4](../../docs/specs/4-connections-runs-and-knowledge.md).

## The box contract

- Keep `prepare` and `run` the only entry points. The platform depends on exactly these two commands.
- Write `prepare`'s stdout as JSON lines only and send every other output to stderr. The platform reads stdout as a protocol, so one stray line breaks a run.
- Keep the Agent SDK's `settingSources` at `['user', 'project']`. The run then reads the box's user settings and the repository's project settings, and the factory decides behavior rather than the host.
- Make each step idempotent. A box can die at any time, and a new one repeats the step from the lease.

## Secrets and reach

- Never put a secret in a prompt, a log line, a file in the repository or a commit. Prompts and logs leave the box.
- Pass a connection's credentials as environment variables of the run's own container. Goald's environment is never the source (ADR 0008).
- Reach Goal only through the run bundle and the gateway's paths. The gateway checks the run's task token, and nothing else from goald is open to a run.
- Never give an agent a user's session or an API key. Those belong to people, and a run is code acting for a factory.

## Tests

- Test `prepare` and `run` with Vitest, faking `exec` and `fetch` at their seams. The box is not available here, and the fakes keep the test on the decision, not the network.
- Assert on the protocol lines and on the values sent, because those are what the platform and the agent see.

## Skills

Vendored skills for this area: claude-api, mcp-builder.

# ADR 0033 — Assembly integration branches: an opt-in branch that collects an assembly's parts and lands at a gate

Status: accepted · 2026-10-28 · Builds on [ADR 0012](0012-assemblies-and-parts.md) (a plan becomes an assembly whose parts ship together) and [ADR 0015](0015-overlapping-in-flight-work.md) (how parts in flight share the default branch)

## Context

An assembly ([ADR 0012](0012-assemblies-and-parts.md)) is a root item whose plan became parts. Each part today is its own item on its own branch (`domain.Item.Branch`, `goal/<number>-<slug>`), and each part's pull request merges into the default branch when the gate at its merge station approves it (`mergeOnApproval`, `internal/core/engine/merge.go`). When the last part is done or cancelled, `closeAssembly` (`internal/adapters/store/postgres/parts.go`) moves the root to done.

So the default branch takes an assembly one part at a time: between the first part's merge and the last one, main holds half a feature. For many factories that is fine ([ADR 0015](0015-overlapping-in-flight-work.md) keeps parts fresh on main). Others want the parts to collect on one branch and reach main together, behind one review.

There is no "planning station" in a definition: a plan is whatever station output carries a non-empty `plan` (`domain.PlanFrom`), and its parts queue at that station.

The work is split into the parts of one assembly (config, store, part bases, landing, worker). This ADR is the design they all follow, so the names below are exact.

## Decision

1. **Integration branches are opt-in, under `board.flow.assemblies.integration`.** `mode` is `trunk` (default) or `branch`; `refresh` is `onLand` (default) or `eachPart`; `minParts` is an integer from 0 (default 0), and an assembly with fewer parts stays on trunk; `landAt` optionally names the landing station. config maps the block into `domain.AssemblyPolicy.Integration` with the defaults applied, and the zero `IntegrationPolicy` means trunk (`IntegrationPolicy.Branches`). `landAt`, when set, must name an agent station with a `gate` and `action: merge`; mode `branch` on a line with no merge station is invalid ([factory-definition.md](../engineering/factory-definition.md#assembly-integration-branches)).
2. **The integration branch is `goal/a<root number>-<root title slug>`** (`domain.IntegrationBranch`), with exactly the slug rules and the 40-character cap of `Item.Branch`, from one shared function.
3. **The landing station is `landAt`, or else the first station with `action: merge` after the station whose plan made the assembly** (`domain.Line.LandingStation`).
4. **The assembly root carries two sticky columns, `integration_branch` and `integration_land_at`, set once when the first part's run starts.** They are set only in mode `branch`, when the assembly has at least `minParts` parts and no pull request yet; once set they never change, so a definition edited mid-flight does not move an assembly that already started. An assembly whose columns stay empty runs on trunk to the end.
5. **A part's base is its assembly's integration branch,** unless the part's pull request was already opened against another base, which it keeps. The agent is told the base, and the worker branches from it.
6. **`closeAssembly` lands a root with an integration branch instead of finishing it.** When the last part is done or cancelled, the root moves to the landing station as `queued`, with an `assembly_landing` event, instead of to done.
7. **The landing run opens a pull request from the integration branch into the default branch, and the gate's approval merges it through `mergeOnApproval`,** like any other item's. An empty landing, where no part changed anything, may be approved with `pull_requests: []`.
8. **A rejected landing goes back to build on the integration branch:** the gate's `onReject` station reworks the root, whose runs work on the integration branch.
9. **Trunk mode is unchanged.** Without the block, or with `mode: trunk`, every part merges into the default branch and `closeAssembly` moves the root to done, as before.

## Consequences

- A factory can ship an assembly to main in one reviewed merge. The default branch never holds half of it.
- Parts in branch mode no longer see each other's work on main but on the integration branch; `refresh` says when that branch takes in main (`onLand`, at landing, or `eachPart`, before each part). Conflicts with main surface at landing, where the landing run resolves them.
- The store (the two columns, `closeAssembly` and the `assembly_landing` event), the engine and gateway (part bases, the agent's prompt, the landing run) and the worker (branching from the bundle's base) each change in their own part. The config part ships first, and nothing reads the policy until those parts do.
- The decision is sticky per assembly, so changing `mode` affects only assemblies whose first part has not started.

# ADR 0043 — Merge stations take turns per target

Status: accepted · 2026-11-06 · Builds on [ADR 0015](0015-overlapping-in-flight-work.md) (the overlap hold, its engine-authored edges and the claim gate of migration 00058) and [ADR 0033](0033-assembly-integration-branches.md) (integration branches and landings)

## Context

A repository whose default branch requires pull requests to be up to date and to pass CI lets one merge through at a time. When several items wait at an agent station with `action: merge`, every free slot claims one, and each integrator runs at once. The first merge moves the default branch; every other pull request must take it in and wait for CI again, 15 to 25 minutes per round. On 2026-11-06 four integrators raced to land on main: one item timed out after two hours with green CI on three successive heads, and integrator runs took 38% of all box time.

The race is per merge target, the branch a pull request merges into. Parts of different assemblies merge into different integration branches and do not race each other; two items landing on main do.

The pieces exist. A station's `merges` setting (`serialize`, the default, or `allow`; `domain.Station.TakesTurns`) says whether the station should take turns. `item_edges` gates claims on any open need (`LockNextWaitingItem` in `db/queries/work_items.sql`), and the overlap hold (`internal/core/engine/overlap.go`) shows how the engine writes edges before a claim and removes the ones whose reason no longer holds.

## Decision

1. **A merge station that takes turns claims at most one item per merge target at a time.** The other items with that target wait in the queue, held by an edge, not blocked. Free slots still take items with other targets. A station with `merges: allow` claims as before.
2. **The merge target of a part is its assembly's integration branch; of every other item, the default branch.** A part of a line in integration mode `branch` is keyed by its assembly. A standalone item, an assembly's landing and a part in mode `trunk` share one key per factory. Items in different repositories that land on their default branches take turns too: targets are not per repository yet.
3. **A turn is an edge of kind `turn` by the engine.** Each waiting item of a target that does not hold the turn gets a `turn` edge on the item that does, so the claim gate skips it until that item is done or cancelled. Migration 00133 adds the kind to `item_edges`.
4. **The holder is picked in a fixed order.** The item of the target in progress at the station, or waiting on a person there (a question or a tool approval, whose pull request stays open until it resumes); else the waiting item the target's turn edges already point to, so an item whose merge was deferred back to the queue keeps its turn; else the first waiting item in pull order that needs nothing open besides turn edges and may start now. Every instance picks the same holder from the same rows.
5. **The hold runs before every claim at the station.** `holdTurns` runs in `claimAt` each time a turn-taking station is tried, not only on its first turn in a tick, so an item that arrived since the last claim of the tick waits too.
6. **A turn edge whose reason no longer holds is removed, with the reason.** The needed item left the station (sent back or moved on), is blocked, has another merge target, or another item holds the turn. The removal is an `edge_removed` event with the reason, as the overlap hold records it. A blocked item never holds a turn, so a blocked integrator does not stall every later landing. An edge the store refuses as a cycle or as one that exists is skipped.

## Consequences

- One integrator per target lands at a time, so its pull request is up to date when its CI passes, and the others wait in the queue without a box.
- Items landing on the default branch wait in line; parallel landing on main is gone for stations that take turns. A factory that wants it back sets `merges: allow` on the station.
- Two instances that claim within the same moment can still both claim an item of one target, when one claims an item that arrived after the other read the queue. The window is one claim wide.
- A turn edge stays on an item a person moves away from the station while it waits, until the holder is done or cancelled: the hold reads only the items at the station.
- The API and the Board explain a turn wait in later work; until then an item's edges carry `turn` outside the API's edge kind enum.
- Per-repository targets, so items landing in different repositories run in parallel, are left for later.

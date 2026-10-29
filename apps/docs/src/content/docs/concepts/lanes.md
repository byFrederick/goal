---
title: Lanes
description: What a lane is, where an item gets its lane, and how a station routes items to an agent by lane.
sidebar:
  order: 4
---

A lane is a route an item takes through a station. A lane lets one station send items of different kinds to different agents: backend work goes to a backend agent, frontend work to a frontend agent, and both use the same station.

A lane is a short key that the factory's definition chooses, such as `backend` or `frontend`. Goal does not have a fixed list of lanes.

## Where an item gets its lane

An item's lane is set in one of three ways.

- **At creation.** A trigger sets it with `board.triggers[].create.lane`, which must be a key of the route at the station the trigger starts the item at. A person can set it when submitting an item.
- **By an output.** When a station finishes an item, its output can set the item's `lane` field. The next station reads it.
- **By a person.** A person can change the lane of an open item through the API or with `goal lane` in the CLI. The change is refused while the item is in progress, because its agent was chosen by the old lane, and for an assembly, whose parts carry their own lanes.

## How a station routes by lane

A station picks its agent in one of two ways. Set `agent` to name one agent for every item. Set `route` to name an agent per lane:

```yaml
route:
  backend: backend-engineer
  frontend: frontend-engineer
```

An entry is either an agent name or an object with `agent` and `agents`. With `agents`, the station keeps that many of its slots for the lane, and the lane uses no other slots. See `board.stations[].route` in the [definition reference](/docs/reference/definition/).

If an item's lane has no entry in a routed station's `route`, the engine blocks the item with the reason. A person can then set a lane that has a route and retry the item.

## Related

- [Stations and the flow](/docs/concepts/stations-and-flow/) explains the stations that route by lane.
- [Assemblies and parts](/docs/concepts/assemblies-and-parts/) explains why a part sets its own lane.
